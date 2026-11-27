package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// maxJSONTreeRows 是 JSON 树一次最多构建的行数；列表是虚拟滚动的，
// 但树本身仍要按这个上限截断，避免超大文档把内存吃满。
const maxJSONTreeRows = 5000

// jsonTreeNode 是 JSON 树的一个节点。渲染时先展平成 jsonTreeRow 列表，
// 交给虚拟列表，折叠只重建列表，不递归构建界面。
type jsonTreeNode struct {
	key      string
	path     string
	depth    int
	kind     string // object / array / string / number / boolean / null / value
	summary  string
	parent   *jsonTreeNode
	children []*jsonTreeNode
}

type jsonTreeRow struct {
	node *jsonTreeNode
	open bool
}

// buildJSONTree 解析 JSON 并展平成树；超过 maxJSONTreeRows 时截断。
func buildJSONTree(data []byte) (*jsonTreeNode, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, false, err
	}
	truncated := false
	count := 0
	var build func(key, path string, depth int, v any) *jsonTreeNode
	build = func(key, path string, depth int, v any) *jsonTreeNode {
		count++
		node := &jsonTreeNode{key: key, path: path, depth: depth}
		switch x := v.(type) {
		case map[string]any:
			node.kind = "object"
			node.summary = fmt.Sprintf("{%d}", len(x))
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if count >= maxJSONTreeRows {
					truncated = true
					break
				}
				child := build(k, path+"/"+k, depth+1, x[k])
				child.parent = node
				node.children = append(node.children, child)
			}
		case []any:
			node.kind = "array"
			node.summary = fmt.Sprintf("[%d]", len(x))
			for i, item := range x {
				if count >= maxJSONTreeRows {
					truncated = true
					break
				}
				child := build(fmt.Sprint(i), fmt.Sprintf("%s[%d]", path, i), depth+1, item)
				child.parent = node
				node.children = append(node.children, child)
			}
		case string:
			node.kind = "string"
			node.summary = jsonQuote(x)
		case json.Number:
			node.kind = "number"
			node.summary = x.String()
		case bool:
			node.kind = "boolean"
			node.summary = "false"
			if x {
				node.summary = "true"
			}
		case nil:
			node.kind = "null"
			node.summary = "null"
		default:
			node.kind = "value"
			b, _ := json.Marshal(x)
			node.summary = string(b)
		}
		return node
	}
	return build("", "", 0, value), truncated, nil
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// applyCellJSON 装载新树：默认只展开根，列表里显示顶层节点。
func (a *root) applyCellJSON(root *jsonTreeNode, truncated bool) {
	a.cellView.JSONRoot = root
	a.cellView.JSONTruncated = truncated
	a.cellView.JSONExpanded = map[string]bool{"": true}
	a.rebuildCellJSONRows()
}

// rebuildCellJSONRows 按当前展开状态生成可见行；折叠的子树整段跳过。
func (a *root) rebuildCellJSONRows() {
	a.cellView.JSONRows = a.cellView.JSONRows[:0]
	if a.cellView.JSONRoot == nil {
		return
	}
	var walk func(n *jsonTreeNode)
	walk = func(n *jsonTreeNode) {
		if len(a.cellView.JSONRows) >= maxJSONTreeRows {
			a.cellView.JSONTruncated = true
			return
		}
		open := len(n.children) > 0 && a.cellView.JSONExpanded[n.path]
		a.cellView.JSONRows = append(a.cellView.JSONRows, jsonTreeRow{node: n, open: open})
		if open {
			for _, child := range n.children {
				walk(child)
			}
		}
	}
	walk(a.cellView.JSONRoot)
}

// expandAllJSON 展开全部可折叠节点；false 时收回到只剩根。
func (a *root) expandAllJSON(open bool) {
	if a.cellView.JSONRoot == nil {
		return
	}
	if !open {
		a.cellView.JSONExpanded = map[string]bool{"": true}
		a.rebuildCellJSONRows()
		return
	}
	var walk func(n *jsonTreeNode)
	walk = func(n *jsonTreeNode) {
		if len(n.children) > 0 {
			a.cellView.JSONExpanded[n.path] = true
			for _, child := range n.children {
				walk(child)
			}
		}
	}
	walk(a.cellView.JSONRoot)
	a.rebuildCellJSONRows()
}

// cellViewFindBar 是 JSON 预览里的搜索条：匹配键名和值，Enter/Shift+Enter 或按钮跳转。
func (a *root) cellViewFindBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 8).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
		input := ui.TextInput(c, &a.cellView.FindQuery).
			Placeholder(locales.CellViewerFindPlaceholder()).
			Label(locales.CellViewerFind()).
			FontSize(12).Grow(1).AutoFocus().
			OnTextKey(func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == ui.Shift:
					a.cellViewFindStep(-1)
					return true
				case k.Key == ui.KeyEnter:
					a.cellViewFindStep(1)
					return true
				case k.Key == ui.KeyEscape:
					a.cellView.FindOpen = false
					return true
				}
				return false
			})
		if input.Changed() {
			a.refreshCellFind()
		}
		switch {
		case strings.TrimSpace(a.cellView.FindQuery) == "":
			ui.Text(c, locales.CellViewerFindHint()).FontSize(11).TextColor(t.TextMuted)
		case len(a.cellView.FindHits) == 0:
			ui.Text(c, locales.CellViewerFindNoMatch()).FontSize(11).TextColor(t.Danger)
		default:
			ui.Text(c, fmt.Sprintf("%d / %d", a.cellView.FindCur+1, len(a.cellView.FindHits))).
				FontSize(11).TextColor(t.TextMuted)
		}
		if iconButtonPlain(c, iconChevronLeft, locales.CellViewerFindPrev(), ui.Transparent).Clicked() {
			a.cellViewFindStep(-1)
		}
		if iconButtonPlain(c, iconChevronRight, locales.CellViewerFindNext(), ui.Transparent).Clicked() {
			a.cellViewFindStep(1)
		}
		if iconButtonPlain(c, iconClose, locales.CommonClose(), ui.Transparent).Clicked() {
			a.cellView.FindOpen = false
		}
	})
}

// refreshCellFind 重新计算匹配：展开命中节点的所有祖先，再重建可见行。
func (a *root) refreshCellFind() {
	q := strings.ToLower(strings.TrimSpace(a.cellView.FindQuery))
	if q == a.cellView.FindLast {
		return
	}
	a.cellView.FindLast = q
	a.cellView.FindHits = a.cellView.FindHits[:0]
	a.cellView.FindCur = 0
	if q == "" || a.cellView.JSONRoot == nil {
		a.rebuildCellJSONRows()
		return
	}
	var walk func(n *jsonTreeNode)
	walk = func(n *jsonTreeNode) {
		if strings.Contains(strings.ToLower(n.key), q) ||
			strings.Contains(strings.ToLower(n.summary), q) {
			a.cellView.FindHits = append(a.cellView.FindHits, n)
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(a.cellView.JSONRoot)
	for _, hit := range a.cellView.FindHits {
		for p := hit.parent; p != nil; p = p.parent {
			a.cellView.JSONExpanded[p.path] = true
		}
	}
	a.rebuildCellJSONRows()
	a.scrollToCellFind()
}

// cellViewFindStep 在匹配之间前后跳转；首尾循环。
func (a *root) cellViewFindStep(delta int) {
	if len(a.cellView.FindHits) == 0 {
		return
	}
	n := len(a.cellView.FindHits)
	a.cellView.FindCur = (a.cellView.FindCur + delta + n) % n
	a.scrollToCellFind()
}

func (a *root) scrollToCellFind() {
	if a.cellView.FindCur < 0 || a.cellView.FindCur >= len(a.cellView.FindHits) {
		return
	}
	hit := a.cellView.FindHits[a.cellView.FindCur]
	for i, row := range a.cellView.JSONRows {
		if row.node == hit {
			a.cellView.JSONList.ScrollTo(i, ui.Center)
			return
		}
	}
}

// jsonRowsView 是 JSON 查看器的虚拟列表：每行一个节点，折叠只改行列表。
func (a *root) jsonRowsView(c *ui.Context) {
	t := c.Theme()
	// 折叠后行数会变少，等列表这一帧画完再重建，避免迭代中的旧下标越界。
	toggle := ""
	ui.List(c, &a.cellView.JSONList, len(a.cellView.JSONRows), func(i int) {
		if i < 0 || i >= len(a.cellView.JSONRows) {
			return
		}
		row := a.cellView.JSONRows[i]
		node := row.node
		root := node.key == ""
		display := node.key
		if root {
			display = "root"
		}
		label := display
		if len(node.children) > 0 {
			label = locales.JsonExpandKey(display)
			if row.open {
				label = locales.JsonCollapseKey(display)
			}
		}
		btn := ui.ButtonBase(c).Label(label).Justify(ui.Start).Padding(1, 4).Radius(3)
		if a.cellView.FindOpen && a.cellView.FindCur < len(a.cellView.FindHits) &&
			a.cellView.FindHits[a.cellView.FindCur] == node {
			btn.Background(t.Accent.Alpha(0.16))
		} else if btn.Hovered() {
			btn.Background(t.SurfaceHover)
		}
		btn.Children(func() {
			ui.Box(c).Width(float32(node.depth) * 14)
			if len(node.children) > 0 {
				icon := iconChevronRight
				if row.open {
					icon = iconChevronDown
				}
				ui.Icon(c, icon).FontSize(11).TextColor(t.TextMuted).Width(14)
			} else {
				ui.Box(c).Width(14)
			}
			if root {
				a.withResultFont(ui.Text(c, "root").Font("monospace").
					FontSize(11.5).TextColor(t.TextMuted).Shrink(0))
			} else {
				// 键名不参与收缩，只让值占剩余宽度并在不够时省略。
				a.withResultFont(ui.Text(c, cutRunes(node.key, 120)).Font("monospace").
					FontSize(11.5).TextColor(t.Accent).SingleLine().Shrink(0))
				a.withResultFont(ui.Text(c, ": ").Font("monospace").
					FontSize(11.5).TextColor(t.TextMuted).Shrink(0))
			}
			a.withResultFont(ui.Text(c, cutRunes(node.summary, 600)).Font("monospace").
				FontSize(11.5).TextColor(jsonTreeValueColor(t, node.kind)).SingleLine().Grow(1))
		})
		if btn.Clicked() && len(node.children) > 0 {
			toggle = node.path
		}
	}).Grow(1).Padding(6, 8)
	if toggle != "" {
		a.cellView.JSONExpanded[toggle] = !a.cellView.JSONExpanded[toggle]
		a.rebuildCellJSONRows()
	}
}

// jsonTreeValueColor 给不同类型的 JSON 值上色：字符串绿、数字/布尔黄、null 灰。
func jsonTreeValueColor(t *ui.Theme, kind string) ui.Color {
	switch kind {
	case "string":
		return t.Success
	case "number", "boolean":
		return t.Warning
	default:
		return t.TextMuted
	}
}
