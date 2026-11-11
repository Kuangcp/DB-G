package ui

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"db-g/app"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// displayToResult 把显示顺序的行号换成结果行下标（排序/筛选后两者不同）。
func (a *root) displayToResult(r *engine.QueryResult, displayRow int) int {
	rows := a.resView.apply(r)
	if displayRow < 0 || displayRow >= len(rows) {
		return -1
	}
	return rows[displayRow]
}

// cursorDataCol 把 Cells.Cursor（列 ID）换成结果里的数据列下标；行号列或未知列返回 -1。
func (a *root) cursorDataCol(r *engine.QueryResult) int {
	id := a.resultState.Cells.Cursor
	if id == "" || id == "#" || r == nil {
		return -1
	}
	for i, name := range r.Columns {
		if name == id {
			return i
		}
	}
	return -1
}

// copyCurrentCell 复制当前选区（多格时按 TSV）或光标所在单元格（Ctrl+C 与「复制」按钮共用）。
func (a *root) copyCurrentCell(c *ui.Context, r *engine.QueryResult) {
	if rows, cols, multi := a.resultSelection(r); multi {
		c.WriteClipboard(rowsTSV(r, rows, cols, false))
		return
	}
	col := a.cursorDataCol(r)
	row := a.displayToResult(r, a.resultRow)
	if r == nil || row < 0 || col < 0 || col >= len(r.Columns) {
		return
	}
	c.WriteClipboard(cellString(r.Rows[row][col]))
}

// resultSelection 返回当前单元格块选中的结果行与数据列下标（都按显示顺序），
// 以及块是否大于 1×1。行号列（#）不算数据列。
func (a *root) resultSelection(r *engine.QueryResult) (rows, cols []int, multi bool) {
	if r == nil {
		return nil, nil, false
	}
	chosen := make(map[int]bool)
	for k := range a.resultSel.All() {
		chosen[k] = true
	}
	for i, ri := range a.resView.apply(r) {
		if chosen[i] {
			rows = append(rows, ri)
		}
	}
	for _, id := range a.resultState.SelectedColumns() {
		if id == "#" {
			continue
		}
		for j, name := range r.Columns {
			if name == id {
				cols = append(cols, j)
				break
			}
		}
	}
	return rows, cols, len(rows) > 1 || len(cols) > 1
}

// blockCSV 把选区写成 CSV（与导出同一套转义；二进制给 base64 摘要）。
func blockCSV(r *engine.QueryResult, rows, cols []int, header bool) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if header {
		names := make([]string, len(cols))
		for i, col := range cols {
			names[i] = r.Columns[col]
		}
		_ = w.Write(names)
	}
	for _, row := range rows {
		rec := make([]string, len(cols))
		for i, col := range cols {
			rec[i] = cellString(cellAt(r.Rows[row], col))
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

func (a *root) resultPane(c *ui.Context) {
	if a.plan.open {
		a.planPane(c)
		return
	}
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
			// 多语句或存在钉住结果时显示结果 tab：pinned 在左，新结果在右。
			if len(a.stmtRuns) > 1 || a.hasPinned() {
				a.stmtTabs(c)
			}
			switch {
			case a.currentRuntime().Running():
				ui.Row(c).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "执行中…")
					if ui.Button(c, "停止").Clicked() {
						a.currentRuntime().Cancel()
					}
				})
			case a.currentRuntime().Err() != "":
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "执行失败").Bold()
					ui.Spacer(c)
					if ui.Button(c, "AI 修复").Clicked() {
						a.fixErrorSQL()
					}
				})
				a.withResultFont(ui.Text(c, a.currentRuntime().Err()).TextColor(c.Theme().Danger).Font("monospace"))
			case a.currentRuntime().Result() != nil:
				r := a.currentRuntime().Result()
				if !r.IsQuery {
					ui.Textf(c, "OK — %d 行受影响", r.Affected)
					return
				}
				if r.Truncated {
					ui.Badge(c, "结果已截断：超过 500 行或 16MB 字符预算")
				}
				a.resultTable(c, r)
			default:
				switch {
				case a.activeSession() == nil:
					ui.Text(c, "请先连接数据源").TextColor(c.Theme().TextMuted)
				case true:
					ui.Text(c, "Ctrl+Enter 执行光标所在语句（选中则只跑选区）").TextColor(c.Theme().TextMuted)
				default:
					ui.Text(c, "Ctrl+Enter 执行选中 SQL").TextColor(c.Theme().TextMuted)
				}
			}
		})
		a.resultFooter(c)
	})
}

// 结果网格的列宽：按值自适应，带下限与硬上限。
const (
	rowHeaderWidth = 56  // 行号列固定宽
	minColumnWidth = 56  // 每列下限，避免空列挤成缝
	maxColumnWidth = 320 // 每列上限（同时限制手动拖拽）
	charWidth      = 8   // 一个半角字符的估算宽度（DIP）
	cellPad        = 24  // 单元格左右内边距
)

// stmtTabs 是结果区的 Tab 条（结果 N · 状态）：pinned 固定在最左，新执行追加到右侧。
// 点击切换当前展示的结果；钉住的结果带图钉标记（对照 db-k 的 ResultChip）。
func (a *root) stmtTabs(c *ui.Context) {
	t := c.Theme()
	ui.ScrollHorizontal(c).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for i := range a.stmtRuns {
				run := a.stmtRuns[i]
				rt := a.runtimeFor(run.runtimeID)
				active := i == a.activeStmt
				label := fmt.Sprintf("结果 %d", run.tabNo)
				state := "成功"
				switch {
				case !run.started:
					state = "未执行"
				case rt.Running():
					state = "执行中"
				case rt.Err() != "":
					state = "失败"
				}
				btn := ui.ButtonBase(c).Label(label+" · "+state).Gap(4).
					Padding(2, 7).Radius(t.Space(1.25))
				if active {
					btn.Background(t.Accent.Alpha(0.14))
				} else if btn.Hovered() {
					btn.Background(t.SurfaceHover)
				}
				btn.Children(func() {
					if run.pinned {
						ui.Icon(c, iconPin).FontSize(12).TextColor(t.Accent)
					}
					txt := ui.Text(c, label).FontSize(12)
					switch {
					case active:
						txt.Bold().TextColor(t.Accent)
					case rt.Err() != "":
						txt.TextColor(t.Danger)
					default:
						txt.TextColor(t.TextMuted)
					}
					// 结束时用状态点代替文案：绿=成功，红=失败；未执行/执行中仍显示文字。
					switch state {
					case "未执行", "执行中":
						ui.Text(c, "· "+state).FontSize(11).TextColor(t.TextMuted)
					case "失败":
						dot := ui.Text(c, "●").FontSize(10).TextColor(t.Danger)
						if err := rt.Err(); err != "" {
							dot.Tooltip(err)
						}
					default:
						ui.Text(c, "●").FontSize(10).TextColor(t.Success)
					}
				})
				if btn.Clicked() {
					a.activeStmt = i
					a.refreshStmtBackgrounds()
				}
			}
		})
	})
}

// dataCols 返回结果数据列的下标（0..n-1）。
func dataCols(r *engine.QueryResult) []int {
	out := make([]int, len(r.Columns))
	for i := range out {
		out[i] = i
	}
	return out
}

// rowTable 返回「复制本行 → INSERT」用的表名：从最近执行语句里取唯一的关系名。
func (a *root) rowTable() (string, bool) {
	rels := relationsIn(a.lastExecSQL)
	seen := map[string]string{}
	for _, r := range rels {
		if r.Table != "" {
			seen[strings.ToLower(r.Table)] = r.Table
		}
	}
	if len(seen) != 1 {
		return "", false
	}
	for _, name := range seen {
		return name, true
	}
	return "", false
}

// connKind 返回当前连接的种类（决定引用符），无连接按 sqlite。
func (a *root) connKind() string {
	if k := a.connKindOf(a.activeConnID()); k != "" {
		return k
	}
	return "sqlite"
}

// openCellViewer 打开单元格完整内容弹窗：合法 JSON 对象/数组渲染成可折叠树，
// 其它内容按原文本显示。
func (a *root) openCellViewer(r *engine.QueryResult, row, col int) {
	if r == nil || row < 0 || row >= len(r.Rows) || col < 0 || col >= len(r.Columns) {
		return
	}
	v := r.Rows[row][col]
	if pending, ok := a.editSet().Cells[app.CellKey{Row: row, Col: col}]; ok {
		v = pending
	}
	text, isJSON := formatCellValue(v)
	a.cellViewTitle = r.Columns[col]
	if isJSON {
		a.cellViewTitle += "（JSON）"
	}
	a.cellViewText = text
	a.fitCellModal(text, isJSON)
	a.cellViewJSON, a.cellViewJSONOK = nil, false
	a.cellViewCollapsed = map[string]bool{}
	// 避免对超大文本做树解析（解析与节点量都随大小爆炸），超出就退回纯文本。
	if isJSON && len(a.cellViewText) <= 1<<20 {
		if v, ok := parseJSONValue(a.cellViewText); ok {
			a.cellViewJSON, a.cellViewJSONOK = v, true
		}
	}
	a.cellViewOpen = true
}

// jsonNodeLimit 是 JSON 树一次最多渲染的节点数，超过后停止展开。
const jsonNodeLimit = 5000

// parseJSONValue 把文本解析成 JSON 树；只接受对象或数组顶层。
func parseJSONValue(text string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if dec.More() {
		return nil, false
	}
	switch v.(type) {
	case map[string]any, []any:
		return v, true
	}
	return nil, false
}

// 单元格查看/编辑弹窗的默认与最小尺寸；窗口再小时由 Modal 的 MaxWidth/MaxHeight 兜底。
const (
	cellModalDefaultW = 880
	cellModalDefaultH = 660
	cellModalMinW     = 360
	cellModalMinH     = 240
)

// cellModalSize 返回查看/编辑弹窗当前尺寸；首次使用时补默认值。
func (a *root) cellModalSize() (float32, float32) {
	if a.cellModalW <= 0 {
		a.cellModalW = cellModalDefaultW
	}
	if a.cellModalH <= 0 {
		a.cellModalH = cellModalDefaultH
	}
	return a.cellModalW, a.cellModalH
}

// fitCellModal 按内容给查看/编辑弹窗一个合适的初始尺寸：短文本用小窗口，
// 大段文本/复杂 JSON 才用默认大窗口；用户仍可拖拽右下角调整。
func (a *root) fitCellModal(text string, isJSON bool) {
	const charW = 7.5
	minW, minH := float32(380), float32(220)
	if isJSON {
		minW, minH = 520, 380
	}
	lines := strings.Split(text, "\n")
	maxRunes, total := 0, 0
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		if n > maxRunes {
			maxRunes = n
		}
		total += n
	}
	if total == 0 {
		a.cellModalW, a.cellModalH = minW, minH
		return
	}
	w := min(max(float32(maxRunes)*charW+100, minW), cellModalDefaultW)
	perLine := max(int((w-60)/charW), 8)
	wrapped := 0
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		wrapped += max((n+perLine-1)/perLine, 1)
	}
	h := min(max(float32(wrapped)*20+110, minH), cellModalDefaultH)
	a.cellModalW, a.cellModalH = w, h
}

// cellModalGrip 是弹窗右下角的拖拽调整大小手柄。面板在窗口里居中，
// 所以右下角要跟住指针，宽高按两倍增量变化。
func (a *root) cellModalGrip(c *ui.Context) {
	a.cellModalSize()
	grip := ui.Box(c).Absolute().Right(2).Bottom(2).Size(18, 18).Cursor(ui.CursorResizeNWSE).
		Label("调整弹窗大小").Tooltip("拖拽调整弹窗大小")
	col := c.Theme().TextMuted
	if grip.Hovered() || grip.Pressed() {
		col = c.Theme().Accent
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		var path ui.Path
		path.MoveTo(r.X+r.W-4, r.Y+r.H-11).LineTo(r.X+r.W-11, r.Y+r.H-4)
		path.MoveTo(r.X+r.W-4, r.Y+r.H-6).LineTo(r.X+r.W-6, r.Y+r.H-4)
		p.StrokePath(&path, 1.5, col)
	})
	if dx, dy, held := grip.Dragged(); held {
		w, h := c.Size()
		a.cellModalW = min(max(a.cellModalW+2*dx, cellModalMinW), w-40)
		a.cellModalH = min(max(a.cellModalH+2*dy, cellModalMinH), h-40)
	}
}

// viewerToolbar 是单元格查看器的标题栏：标题在左，JSON 折叠控制与复制/关闭在右。
func (a *root) viewerToolbar(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		ui.Text(c, a.cellViewTitle).Bold().SingleLine()
		ui.Spacer(c)
		if a.cellViewJSONOK {
			if iconButtonPlain(c, iconCollapseAll, "收起全部层级到第一层", ui.Transparent).Clicked() {
				a.collapseAllJSON()
			}
			if iconButtonPlain(c, iconExpandAll, "展开全部层级", ui.Transparent).Clicked() {
				a.expandAllJSON()
			}
		} else {
			label := "自动折行：开（点击关闭）"
			col := c.Theme().Accent
			if !a.cellViewWrap {
				label, col = "自动折行：关（点击开启）", ui.Transparent
			}
			if iconButtonPlain(c, iconWrapText, label, col).Clicked() {
				a.cellViewWrap = !a.cellViewWrap
			}
		}
		if iconButtonPlain(c, iconCopy, "复制原文", ui.Transparent).Tooltip("复制完整原文（未折叠的 JSON 文本）").Clicked() {
			c.WriteClipboard(a.cellViewText)
		}
		if iconButtonPlain(c, iconClose, "关闭", ui.Transparent).Clicked() {
			a.cellViewOpen = false
		}
	})
}

// expandAllJSON 清空折叠状态，让所有 JSON 节点展开。
func (a *root) expandAllJSON() {
	a.cellViewCollapsed = make(map[string]bool)
}

// collapseAllJSON 只保留第一层展开，其他所有子层级折叠。
func (a *root) collapseAllJSON() {
	paths := collectAllPaths(a.cellViewJSON)
	col := make(map[string]bool, len(paths))
	for _, p := range paths {
		// $ .key 或 $ [0] 是根的直接子节点，保留展开
		// 更深一层的才折叠：需要跳过以 $.key.、$ [i]、$ .key.[j 开头但实际要排除的
		if isTopLevel(p) {
			continue
		}
		col[p] = true
	}
	a.cellViewCollapsed = col
}

// isTopLevel 判断 path 是否为 JSON 根的直接子节点。
// 路径格式：$ .key → 第二层（顶层子节点），$[0] → 同上；$.a.b 或 $[0].b → 深层。
func isTopLevel(path string) bool {
	rest := strings.TrimPrefix(path, "$")
	// $ 本身不是子节点
	if rest == "" {
		return false
	}
	// $.key — 第一个 "." 后面没有更多 "." 且不在 "[" 前
	if idx := strings.IndexByte(rest, '.'); idx >= 0 {
		// 检查 "." 之后是否还有 "." 或 "]"
		after := rest[idx+1:]
		return !strings.ContainsAny(after, ".[")
	}
	// $[i] — 单个 "]" 结尾即为顶层
	if rest[0] == '[' {
		closeIdx := strings.IndexByte(rest, ']')
		if closeIdx < 0 {
			return false
		}
		remaining := rest[closeIdx+1:]
		return remaining == ""
	}
	return false
}

// collectAllPaths 递归收集 JSON 中所有可折叠节点（map/array）的 path。
func collectAllPaths(v any) []string {
	var out []string
	var walk func(any, string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			for k, v := range n {
				p := path + "." + k
				out = append(out, p)
				walk(v, p)
			}
		case []any:
			for i, e := range n {
				p := path + "[" + strconv.Itoa(i) + "]"
				out = append(out, p)
				walk(e, p)
			}
		}
	}
	walk(v, "$")
	return out
}

// cellViewer 渲染长文本查看弹窗；JSON 时画可展开/折叠的树。
func (a *root) cellViewer(c *ui.Context) {
	if !a.cellViewOpen {
		return
	}
	w, h := a.cellModalSize()
	ui.Modal(c, &a.cellViewOpen, func() {
		a.viewerToolbar(c)
		switch {
		case a.cellViewJSONOK:
			ui.ScrollBoth(c).Grow(1).Children(func() {
				nodes := 0
				a.jsonTree(c, "$", a.cellViewJSON, 0, &nodes)
			})
		case a.cellViewWrap:
			// 纵向滚动容器里再套一层 FillWidth，文本才会拿到视口宽度并折行。
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					a.withResultFont(ui.Text(c, a.cellViewText).Font("monospace").FontSize(12))
				})
			})
		default:
			// 不折行：整行保留自然宽度，交给横向滚动查看。
			ui.ScrollBoth(c).Grow(1).Children(func() {
				a.withResultFont(ui.Text(c, a.cellViewText).Font("monospace").FontSize(12).NoWrap())
			})
		}
		a.cellModalGrip(c)
	}).Width(w).Height(h)
}

// jsonTree 递归渲染 JSON 节点；path 是折叠状态的键。
func (a *root) jsonTree(c *ui.Context, path string, v any, depth int, nodes *int) {
	if *nodes >= jsonNodeLimit {
		ui.Text(c, "…节点过多，只显示前 "+strconv.Itoa(jsonNodeLimit)+" 个").TextColor(c.Theme().TextMuted).FontSize(12)
		return
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			*nodes++
			a.jsonEntry(c, path+"."+k, k, t[k], depth, nodes)
		}
	case []any:
		for i, e := range t {
			*nodes++
			a.jsonEntry(c, path+"["+strconv.Itoa(i)+"]", strconv.Itoa(i), e, depth, nodes)
		}
	}
}

// jsonEntry 渲染一个键（或数组下标）及其值；对象/数组可点击折叠。
func (a *root) jsonEntry(c *ui.Context, path, key string, v any, depth int, nodes *int) {
	t := c.Theme()
	indent := ui.Row(c).MarginStart(float32(depth) * 16)
	var kids map[string]any
	var list []any
	switch x := v.(type) {
	case map[string]any:
		kids = x
	case []any:
		list = x
	}
	if kids != nil || list != nil {
		open := !a.cellViewCollapsed[path]
		label := "展开：" + key
		if open {
			label = "收起：" + key
		}
		indent.Gap(4).AlignItems(ui.Center).Children(func() {
			btn := ui.ButtonBase(c).Label(label).Padding(t.Space(0.5)).Radius(t.Space(0.75))
			if btn.Hovered() {
				btn.Background(t.SurfaceHover)
			}
			btn.Children(func() {
				ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
				if !open {
					ic.Rotate(-90)
				}
			})
			if btn.Clicked() {
				if open {
					a.cellViewCollapsed[path] = true
				} else {
					delete(a.cellViewCollapsed, path)
				}
			}
			ui.Text(c, key).FontSize(12.5).Bold()
			summary := fmt.Sprintf("{ %d 项 }", len(kids))
			if list != nil {
				summary = fmt.Sprintf("[ %d 项 ]", len(list))
			}
			ui.Text(c, summary).FontSize(11.5).TextColor(t.TextMuted)
		})
		if open {
			a.jsonTree(c, path, v, depth+1, nodes)
		}
		return
	}
	indent.Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(12)
		ui.Text(c, key+":").FontSize(12.5).TextColor(t.TextMuted)
		text, col := jsonLeafText(v, t)
		a.withResultFont(ui.Text(c, text).FontSize(12.5).Font("monospace").TextColor(col).SingleLine())
	})
}

// jsonLeafText 返回叶子值的展示文本与颜色。
func jsonLeafText(v any, t *ui.Theme) (string, ui.Color) {
	switch x := v.(type) {
	case nil:
		return "null", t.TextMuted
	case bool:
		return strconv.FormatBool(x), t.Success
	case json.Number:
		return x.String(), t.Accent
	case string:
		return `"` + cutRunes(x, 200) + `"`, t.Text
	default:
		return fmt.Sprint(x), t.Text
	}
}

func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	widths := a.columnWidths(r)
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: rowHeaderWidth, Fixed: true, Align: ui.End})
	for i, name := range r.Columns {
		cols = append(cols, ui.TableColumn{
			Title:    name,
			ID:       name,
			Width:    widths[i],
			MinWidth: minColumnWidth,
			MaxWidth: maxColumnWidth,
		})
	}
	a.ensureEditState(r)
	displayRows := a.resView.apply(r)
	editSet := a.editSet()
	insertRows := len(editSet.Inserts)
	totalRows := len(displayRows) + insertRows

	// 顶部操作条：刷新/取更多/钉住/编辑。工具条里的按钮悬停前没有面孔。
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Toolbar(c, func() {
			if sql, ok := a.rerunnableSQL(); ok {
				if iconButton(c, iconRefresh, "重新执行此查询", ui.Transparent).
					Disabled(a.anyRunRunning() || a.activeSession() == nil).Clicked() {
					a.rerunQuery(sql)
				}
			}
			if iconButton(c, iconChevronDown, "取更多行", ui.Transparent).Disabled(!a.canFetchMore()).Clicked() {
				a.fetchMore()
			}
			a.editToolbar(c, r)
		}).Label("结果操作")
		ui.Divider(c)
		a.pinButton(c)
		iconButtonPlain(c, iconDownload, "导出…", ui.Transparent).Menu(func(m *ui.Menu) {
			if m.Item("当前结果 CSV").Chosen() {
				a.pickExport("csv", false)
			}
			if m.Item("当前结果 JSON").Chosen() {
				a.pickExport("json", false)
			}
			if m.Item("当前结果 SQL INSERT").Chosen() {
				a.pickExport("insert", false)
			}
			full := a.canStream()
			if it := m.Item("全量 CSV（流式，重新执行查询）"); it.Disabled(!full).Chosen() {
				a.confirmFullExport("csv")
			}
			if it := m.Item("全量 JSON（流式，重新执行查询）"); it.Disabled(!full).Chosen() {
				a.confirmFullExport("json")
			}
		})
		if a.exportStatus != "" {
			ui.Text(c, a.exportStatus).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
			if iconButtonPlain(c, iconClose, "清除导出提示", ui.Transparent).Clicked() {
				a.exportStatus = ""
			}
		}
		if n := len(a.resView.filters); n > 0 {
			ui.Text(c, fmt.Sprintf("筛选 %d 列", n)).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		//ui.Text(c, "右键表头排序/筛选；右键单元格/选区复制与导出").FontSize(12).TextColor(c.Theme().TextMuted)
	})

	a.resultState.HeaderMenu = func(m *ui.Menu, col int) {
		if col <= 0 {
			return
		}
		idx := col - 1
		if m.Item("升序").Chosen() {
			a.resView.sortBy(r, idx, false)
		}
		if m.Item("降序").Chosen() {
			a.resView.sortBy(r, idx, true)
		}
		if m.Item("清除排序").Chosen() {
			a.resView.sortBy(r, -1, false)
		}
		if m.Item("筛选…").Chosen() {
			a.filterOpen, a.filterCol, a.filterText = true, idx, a.resView.filters[idx]
		}
		if _, has := a.resView.filters[idx]; has {
			if m.Item("清除本列筛选").Chosen() {
				a.resView.clearFilter(idx)
			}
		}
	}

	// 单元格级选择：Selection 与 Selected 同时设置才真正进入 CellSelect
	// （mygo 要求两者都有），点击会同时移动行与列光标。
	if a.lastResult != r {
		a.lastResult = r
		a.resultSel.Clear()
		a.stopInlineEdit()
		// 列光标默认落在第一列数据（不是行号列），未点击时的复制/查看也有目标。
		a.resultState.Cells = ui.ColumnRange{}
		if len(r.Columns) > 0 {
			a.resultState.Cells = ui.ColumnRange{Cursor: r.Columns[0], Anchor: r.Columns[0]}
		}
	}
	a.resultState.Selection = &a.resultSel
	a.resultState.CellSelect = true
	a.resultState.Selected = &a.resultRow
	a.resultState.CellsStyle = ui.CellsStyle{
		Cell:     c.Theme().Accent,
		CellText: c.Theme().AccentText,
		Row:      c.Theme().Accent.Alpha(0.12),
		Stripe:   c.Theme().Text.Alpha(0.035),
	}
	a.resultState.CellMenu = func(m *ui.Menu, row, col int) {
		// resultRow 与 Table 一致，存「显示顺序」；取数据时再映射到结果行。
		a.resultRow = row
		row = a.displayToResult(r, row)
		dataCol := col - 1 // 第 0 列是行号
		if dataCol >= 0 && dataCol < len(r.Columns) {
			if m.Item("复制单元格").Chosen() {
				c.WriteClipboard(cellString(r.Rows[row][dataCol]))
			}
		}
		if m.Item("复制本行").Chosen() {
			c.WriteClipboard(rowsTSV(r, []int{row}, dataCols(r), false))
		}
		if it := m.Item("复制本行 → INSERT"); it.Chosen() {
			if table, ok := a.rowTable(); ok {
				c.WriteClipboard(insertSQL(a.connKind(), table, r.Columns, r.Rows[row]))
			}
		} else if table, ok := a.rowTable(); !ok {
			_ = table
		}
		if rows, cols, multi := a.resultSelection(r); multi {
			m.Submenu("复制选区", func(sm *ui.Menu) {
				if sm.Item("TSV").Chosen() {
					c.WriteClipboard(rowsTSV(r, rows, cols, false))
				}
				if sm.Item("TSV（含表头）").Chosen() {
					c.WriteClipboard(rowsTSV(r, rows, cols, true))
				}
				if sm.Item("CSV").Chosen() {
					c.WriteClipboard(blockCSV(r, rows, cols, false))
				}
			})
			_, hasTable := a.rowTable()
			m.Submenu("导出选区", func(sm *ui.Menu) {
				if sm.Item("CSV…").Chosen() {
					a.pickExportSelection("csv", rows, cols)
				}
				if sm.Item("JSON…").Chosen() {
					a.pickExportSelection("json", rows, cols)
				}
				if it := sm.Item("SQL INSERT…"); it.Disabled(!hasTable).Chosen() {
					a.pickExportSelection("insert", rows, cols)
				}
			})
		}
		if dataCol >= 0 && m.Item("查看完整内容").Chosen() {
			a.openCellViewer(r, row, dataCol)
		}
		if a.editPlan != nil && dataCol >= 0 {
			if it := m.Item("编辑单元格…"); it.Disabled(!a.editPlan.Editable[dataCol]).Chosen() {
				a.editCellRow, a.editCellCol = row, dataCol
				a.editCellText = cellString(cellValueOf(r, row, dataCol))
				a.editCellOpen = true
			}
			if a.editPlan.Editable[dataCol] && m.Item("设为 NULL").Chosen() {
				a.setCellEdit(row, dataCol, nil)
			}
			if _, has := a.editSet().Cells[app.CellKey{Row: row, Col: dataCol}]; has {
				if m.Item("还原本格").Chosen() {
					a.editSet().ClearCell(row, dataCol)
				}
			}
		}
		if a.editPlan != nil {
			if a.editSet().Deletes[row] {
				if m.Item("还原本行").Chosen() {
					a.clearRowEdit(row)
				}
			} else if m.Item("删除本行").Chosen() {
				a.deleteRow(row)
			}
		}
	}

	handledDouble := false
	tbl := a.withResultFont(ui.Table(c, &a.resultState, cols, totalRows, func(row, col int) {
		if row < 0 {
			return
		}
		// 新增行渲染在数据行之后。
		if row >= len(displayRows) {
			insIdx := row - len(displayRows)
			if insIdx >= len(editSet.Inserts) {
				return
			}
			if col == 0 {
				ui.Text(c, "+").TextColor(c.Theme().Success)
				return
			}
			v, ok := editSet.Inserts[insIdx][col-1]
			if !ok {
				ui.Text(c, "").SingleLine()
				return
			}
			ui.Text(c, cellText(v)).SingleLine().TextColor(c.Theme().Success)
			return
		}
		ri := displayRows[row]
		if col == 0 {
			label := strconv.Itoa(ri + 1)
			if editSet.Deletes[ri] {
				ui.Text(c, "−"+label).TextColor(c.Theme().Danger)
				return
			}
			ui.Text(c, label).TextColor(c.Theme().TextMuted)
			return
		}
		if col-1 >= len(r.Rows[ri]) {
			return
		}
		if a.inlineEditOn && a.inlineEditRow == ri && a.inlineEditCol == col-1 {
			a.inlineCellEditor(c)
			return
		}
		v := r.Rows[ri][col-1]
		if pending, ok := editSet.Cells[app.CellKey{Row: ri, Col: col - 1}]; ok {
			v = pending
		}
		txt := ui.Text(c, cellText(v)).SingleLine()
		if editSet.Deletes[ri] {
			txt.TextColor(c.Theme().TextMuted)
			return
		}
		if _, dirty := editSet.Cells[app.CellKey{Row: ri, Col: col - 1}]; dirty {
			if row == a.resultRow && col-1 == a.cursorDataCol(r) {
				txt.TextColor(c.Theme().AccentText).Bold()
			} else {
				txt.TextColor(c.Theme().Accent).Bold()
			}
		}
		if txt.RowDoubleClicked() {
			handledDouble = true
			if c.Modifiers()&ui.Ctrl != 0 {
				a.beginEditAt(r, ri, col-1)
			} else {
				a.openCellViewer(r, ri, col-1)
			}
		}
	})).Grow(1)
	if tbl.Submitted() && !handledDouble {
		row := a.displayToResult(r, a.resultRow)
		col := a.cursorDataCol(r)
		if c.Modifiers()&ui.Ctrl != 0 {
			a.beginEditAt(r, row, col)
		} else {
			a.openCellViewer(r, row, col)
		}
	}
}

// pinButton 显示钉住按钮：当前结果已钉住时图标上强调色，再点一次取消。
func (a *root) pinButton(c *ui.Context) {
	pinned := len(a.stmtRuns) > 0 && a.activeStmt < len(a.stmtRuns) && a.stmtRuns[a.activeStmt].pinned
	label := "钉住结果"
	col := ui.Transparent
	if pinned {
		label = "取消钉住"
		col = c.Theme().Accent
	}
	if iconButtonPlain(c, iconPin, label, col).Clicked() {
		a.togglePin()
	}
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		// []byte 既可能是 MySQL 的 TEXT/VARCHAR（应渲染文本），也可能是真正的二进制
		// （BLOB）。合法 UTF-8 且无 NUL 时按文本渲染，否则给字节数摘要。
		if isTextBytes(t) {
			return cutRunes(string(t), 512)
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return cutRunes(t, 512)
	default:
		return fmt.Sprint(t)
	}
}

// isTextBytes 判断字节串是否可以当文本显示：合法 UTF-8，且不含 NUL 等控制字符
// （制表/换行除外，它们由表格单行渲染处理）。
func isTextBytes(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == 0 {
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func cutRunes(s string, n int) string {
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if i >= len(s) {
		return s
	}
	return s[:i] + "…"
}

// columnWidths 返回结果各列的宽度（按值自适应，带缓存：同一结果只量一次）。
func (a *root) columnWidths(r *engine.QueryResult) []float32 {
	if a.widthsFor == r && len(a.widths) == len(r.Columns) {
		return a.widths
	}
	a.widthsFor = r
	a.widths = resultWidths(r)
	return a.widths
}

// resultWidths 按表头与每一行渲染后的文本量出各列宽度。
func resultWidths(r *engine.QueryResult) []float32 {
	out := make([]float32, len(r.Columns))
	for i := range r.Columns {
		units := displayWidth(r.Columns[i])
		for _, row := range r.Rows {
			if i >= len(row) {
				continue
			}
			if u := displayWidth(cellText(row[i])); u > units {
				units = u
			}
		}
		w := float32(units*charWidth + cellPad)
		out[i] = min(max(w, minColumnWidth), maxColumnWidth)
	}
	return out
}

// displayWidth 估算字符串的显示宽度：半角 1 个单位，中日韩/全角 2 个单位。
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == 0 || r == '\n' || r == '\r' || r == '\t':
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x1100 && (r <= 0x115f || // Hangul Jamo
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || // CJK 部首/假名/汉字
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul 音节
		(r >= 0xf900 && r <= 0xfaff) || // CJK 兼容汉字
		(r >= 0xfe30 && r <= 0xfe6f) || // CJK 兼容形式
		(r >= 0xff00 && r <= 0xff60) || // 全角 ASCII
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f9ff) || // emoji
		(r >= 0x20000 && r <= 0x3fffd)): // CJK 扩展
		return 2
	}
	return 1
}

// canFetchMore 报告当前结果能否「取更多」：单条查询、驱动支持、还有更多行。
func (a *root) canFetchMore() bool {
	rt := a.currentRuntime()
	r := rt.Result()
	if r == nil || !r.IsQuery || rt.Running() || a.activeSession() == nil ||
		!a.activeSession().Caps().Has(engine.CapFetchMore) {
		return false
	}
	if _, ok := fetchMoreSQL(a.lastExecSQL, 0, 1); !ok {
		return false
	}
	st, ok := a.fetchStates[a.currentRuntimeID()]
	return ok && st.more
}

// fetchMore 用派生表分页把后续行追加到当前结果（不重跑原 SQL 的第一页）。
func (a *root) fetchMore() {
	if !a.canFetchMore() {
		return
	}
	id := a.currentRuntimeID()
	st := a.fetchStates[id]
	sql, ok := fetchMoreSQL(a.lastExecSQL, st.loaded, 500)
	if !ok {
		a.consoleErr = "取更多失败：当前语句不支持分页"
		return
	}
	base := a.currentRuntime()
	more := a.runtimeFor(id + "#more")
	more.SetOnDone(func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		apply := func() {
			if errMsg != "" {
				a.consoleErr = "取更多失败: " + errMsg
				return
			}
			if res == nil || base.Result() == nil {
				return
			}
			r := base.Result()
			r.Rows = append(r.Rows, res.Rows...)
			r.Truncated = res.Truncated
			a.fetchStates[id] = fetchState{loaded: len(r.Rows), more: res.Truncated && res.IsQuery}
			a.resView.key = ""
		}
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, apply)
			return
		}
		apply()
	})
	if !more.Run(a.activeSession(), sql) {
		a.consoleErr = "取更多失败：无法启动"
	}
}

// filterModal 渲染每列筛选输入。
func (a *root) filterModal(c *ui.Context) {
	if !a.filterOpen {
		return
	}
	ui.Modal(c, &a.filterOpen, func() {
		title := "筛选"
		if r := a.currentRuntime().Result(); r != nil && a.filterCol >= 0 && a.filterCol < len(r.Columns) {
			title = "筛选：" + r.Columns[a.filterCol]
		}
		ui.Text(c, title).Bold()
		ui.TextInput(c, &a.filterText).Placeholder("包含…（留空清除）").AutoFocus()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.filterOpen = false
			}
			if ui.PrimaryButton(c, "应用").Clicked() {
				a.resView.setFilter(a.filterCol, a.filterText)
				a.filterOpen = false
			}
		})
	})
}

// recordFetchState 在语句执行成功后记录分页状态。
func (a *root) recordFetchState(runtimeID string, res *engine.QueryResult) {
	if res == nil || !res.IsQuery {
		delete(a.fetchStates, runtimeID)
		return
	}
	a.fetchStates[runtimeID] = fetchState{loaded: len(res.Rows), more: res.Truncated}
	a.resView.key = ""
}

// canStream 报告能否全量流式导出（驱动实现 RowStreamer 且有可重放的 SQL）。
func (a *root) canStream() bool {
	sess := a.activeSession()
	if sess == nil || strings.TrimSpace(a.lastExecSQL) == "" {
		return false
	}
	_, ok := sess.(engine.RowStreamer)
	return ok
}

// confirmFullExport 打开「会重新执行查询」的确认弹窗。
func (a *root) confirmFullExport(format string) {
	a.exportFormat = format
	a.exportConfirm = true
}

// exportConfirmModal 渲染全量导出确认。
func (a *root) exportConfirmModal(c *ui.Context) {
	if !a.exportConfirm {
		return
	}
	ui.Modal(c, &a.exportConfirm, func() {
		ui.Text(c, "全量导出（"+a.exportFormat+"）").Bold()
		ui.Text(c, "会重新执行当前语句读取全部结果，数据可能与屏幕上略有差异。").TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.exportConfirm = false
			}
			if ui.PrimaryButton(c, "选择文件…").Clicked() {
				format := a.exportFormat
				a.exportConfirm = false
				a.pickExport(format, true)
			}
		})
	})
}

// pickExport 弹保存对话框（goroutine），选定后后台导出。
func (a *root) pickExport(format string, full bool) {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		ext := "." + format
		if format == "insert" {
			ext = ".sql"
		}
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "导出结果",
			DefaultPath: "result" + ext,
		})
		if err != nil || path == "" {
			return
		}
		a.safeUpdate(win, func() {
			a.exportStatus = "导出中…"
		})
		n, err := a.exportTo(path, format, full)
		a.safeUpdate(win, func() {
			if err != nil {
				a.exportStatus = "导出失败: " + err.Error()
				app.LogError("export", "failed", "path", path, "format", format, "err", err.Error())
				return
			}
			a.exportStatus = fmt.Sprintf("已导出 %d 行 → %s", n, path)
			app.LogInfo("export", "done", "path", path, "format", format, "rows", n, "full", full)
		})
	}()
}

// pickExportSelection 弹保存对话框导出当前选区（行/列为结果下标）。
func (a *root) pickExportSelection(format string, rows, cols []int) {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		ext := "." + format
		if format == "insert" {
			ext = ".sql"
		}
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "导出选区",
			DefaultPath: "selection" + ext,
		})
		if err != nil || path == "" {
			return
		}
		a.safeUpdate(win, func() { a.exportStatus = "导出中…" })
		n, err := a.exportSelectionTo(path, format, rows, cols)
		a.safeUpdate(win, func() {
			if err != nil {
				a.exportStatus = "导出失败: " + err.Error()
				app.LogError("export", "selection failed", "path", path, "format", format, "err", err.Error())
				return
			}
			a.exportStatus = fmt.Sprintf("已导出选区 %d 行 → %s", n, path)
			app.LogInfo("export", "selection done", "path", path, "format", format, "rows", n)
		})
	}()
}

// exportSelectionTo 同步导出选区（对话框之外的实现；测试直接调用）。
func (a *root) exportSelectionTo(path, format string, rows, cols []int) (int, error) {
	r := a.currentRuntime().Result()
	if r == nil || !r.IsQuery {
		return 0, errors.New("没有可导出的查询结果")
	}
	table := ""
	if t, ok := a.rowTable(); ok {
		table = t
	}
	if format == "insert" && table == "" {
		return 0, errors.New("无法确定表名（多表查询），不能导出 INSERT")
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = r.Columns[col]
	}
	data := make([][]any, len(rows))
	for i, row := range rows {
		rec := make([]any, len(cols))
		for j, col := range cols {
			rec[j] = cellAt(r.Rows[row], col)
		}
		data[i] = rec
	}
	return app.ExportRows(path, format, a.connKind(), table, names, data)
}

// exportTo 同步导出（对话框之外的实现；测试直接调用）。
func (a *root) exportTo(path, format string, full bool) (int, error) {
	kind := a.connKind()
	table := ""
	if _, ok := fetchMoreSQL(a.lastExecSQL, 0, 1); ok {
		if t, ok := a.rowTable(); ok {
			table = t
		}
	}
	if format == "insert" && table == "" {
		return 0, errors.New("无法确定表名（多表查询），不能导出 INSERT")
	}
	if full {
		streamer, ok := a.activeSession().(engine.RowStreamer)
		if !ok {
			return 0, errors.New("当前数据源不支持流式导出")
		}
		sql := a.lastExecSQL
		stream := func(onCols func([]string) error, onRow func([]any) error) error {
			return streamer.Stream(context.Background(), sql, onCols, onRow)
		}
		return app.ExportStream(path, format, kind, table, stream)
	}
	r := a.currentRuntime().Result()
	if r == nil || !r.IsQuery {
		return 0, errors.New("没有可导出的查询结果")
	}
	rows := make([][]any, len(r.Rows))
	copy(rows, r.Rows)
	return app.ExportRows(path, format, kind, table, r.Columns, rows)
}
