package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// connTagDraft 是连接弹窗里一个已添加标签的编辑态。
type connTagDraft struct {
	label string
	color string
}

// resetConnTagForm 清空标签列表与输入框，颜色回到默认蓝。
func (a *root) resetConnTagForm() {
	a.connFormTags = nil
	a.connFormTagInput = ""
	a.connFormTagColor = defaultTagColor(0)
}

// setConnTagForm 用档案里的标签填充编辑列表。
func (a *root) setConnTagForm(tags []app.ConnTag) {
	a.resetConnTagForm()
	for _, tag := range app.NormalizeTags(tags) {
		a.connFormTags = append(a.connFormTags, connTagDraft{label: tag.Label, color: tag.Color})
	}
}

// connTagFormList 把编辑列表读成数据层标签。
func (a *root) connTagFormList() []app.ConnTag {
	tags := make([]app.ConnTag, 0, len(a.connFormTags))
	for _, tag := range a.connFormTags {
		tags = append(tags, app.ConnTag{Label: tag.label, Color: tag.color})
	}
	return app.NormalizeTags(tags)
}

// addConnFormTag 添加一个标签：去空、去重（忽略大小写）、最多 MaxConnTags 个。
// color 为空时用输入框左侧色点当前的颜色。
func (a *root) addConnFormTag(label, color string) bool {
	label = strings.TrimSpace(label)
	if label == "" || len(a.connFormTags) >= app.MaxConnTags {
		return false
	}
	for _, tag := range a.connFormTags {
		if strings.EqualFold(tag.label, label) {
			return false
		}
	}
	if color == "" {
		color = a.connFormTagColor
	}
	a.connFormTags = append(a.connFormTags, connTagDraft{label: label, color: color})
	a.connFormTagInput = ""
	return true
}

// removeConnFormTag 移除已添加的标签（调用方保证 label 来自当前列表）。
func (a *root) removeConnFormTag(label string) {
	for i, tag := range a.connFormTags {
		if strings.EqualFold(tag.label, label) {
			a.connFormTags = append(a.connFormTags[:i], a.connFormTags[i+1:]...)
			return
		}
	}
}

// tagColorByLabel 在已有数据源里找同名标签的颜色；找不到返回空串（用当前色点颜色）。
func (a *root) tagColorByLabel(label string) string {
	if a.store == nil {
		return ""
	}
	for _, p := range a.store.List() {
		for _, tag := range p.Tags {
			if strings.EqualFold(tag.Label, label) {
				return tag.Color
			}
		}
	}
	return ""
}

// tagSuggestions 返回可复用的已有标签：排除当前已选的，按「前缀匹配 → 包含匹配」
// 排序；查询为空时给全部。同名标签只保留第一次出现的颜色。
func (a *root) tagSuggestions() []app.ConnTag {
	if a.store == nil {
		return nil
	}
	selected := make(map[string]bool, len(a.connFormTags))
	for _, tag := range a.connFormTags {
		selected[strings.ToLower(tag.label)] = true
	}
	query := strings.ToLower(strings.TrimSpace(a.connFormTagInput))
	seen := map[string]bool{}
	var starts, contains []app.ConnTag
	for _, p := range a.store.List() {
		for _, tag := range p.Tags {
			key := strings.ToLower(tag.Label)
			if selected[key] || seen[key] {
				continue
			}
			seen[key] = true
			switch {
			case query == "" || strings.HasPrefix(key, query):
				starts = append(starts, tag)
			case strings.Contains(key, query):
				contains = append(contains, tag)
			}
		}
	}
	return append(starts, contains...)
}

// tagColorSpec 是标签调色板的一项：Key 落盘，Label 给人看，Color 是实际颜色。
type tagColorSpec struct {
	Key   string
	Label string
	Color ui.Color
}

// tagColorSpecs 是固定调色板。颜色为中等明度，浅色/深色主题下都能辨认；
// 标签只存 Key，主题切换不影响已保存的标签。
var tagColorSpecs = []tagColorSpec{
	{Key: "red", Label: "红", Color: ui.Hex("#e5484d")},
	{Key: "orange", Label: "橙", Color: ui.Hex("#f76b15")},
	{Key: "amber", Label: "金", Color: ui.Hex("#e5a50a")},
	{Key: "green", Label: "绿", Color: ui.Hex("#30a46c")},
	{Key: "teal", Label: "青", Color: ui.Hex("#12a594")},
	{Key: "blue", Label: "蓝", Color: ui.Hex("#0091ff")},
	{Key: "violet", Label: "紫", Color: ui.Hex("#8e4ec6")},
	{Key: "pink", Label: "粉", Color: ui.Hex("#d6409f")},
	{Key: "gray", Label: "灰", Color: ui.Hex("#8b8d98")},
}

func tagColorSpecOf(key string) tagColorSpec {
	for _, spec := range tagColorSpecs {
		if spec.Key == key {
			return spec
		}
	}
	for _, spec := range tagColorSpecs {
		if spec.Key == app.DefaultConnTagColor {
			return spec
		}
	}
	return tagColorSpecs[0]
}

// tagColor 把落盘的调色板键换成颜色；未知键回落默认蓝。
func tagColor(key string) ui.Color { return tagColorSpecOf(key).Color }

// tagColorLabel 把落盘的调色板键换成中文色名。
func tagColorLabel(key string) string { return tagColorSpecOf(key).Label }

// nextTagColor 返回调色板里的下一个键，用于点色块循环换色。
func nextTagColor(key string) string {
	spec := tagColorSpecOf(key)
	for i, s := range tagColorSpecs {
		if s.Key == spec.Key {
			return tagColorSpecs[(i+1)%len(tagColorSpecs)].Key
		}
	}
	return app.DefaultConnTagColor
}

// defaultTagColor 返回第 i 个标签槽的默认颜色：蓝 / 绿 / 橙。
func defaultTagColor(i int) string {
	keys := []string{"blue", "green", "orange"}
	if i >= 0 && i < len(keys) {
		return keys[i]
	}
	return app.DefaultConnTagColor
}

// tagChip 是只读标签胶囊：彩色圆点 + 标签名，底色取同色低透明度。
func tagChip(c *ui.Context, tag app.ConnTag) {
	t := c.Theme()
	col := tagColor(tag.Color)
	ui.Row(c).Gap(4).AlignItems(ui.Center).
		Padding(1, 7).Radius(t.Space(1)).
		Background(col.Alpha(0.14)).
		Children(func() {
			ui.Text(c, "●").FontSize(8).TextColor(col)
			ui.Text(c, tag.Label).FontSize(11).TextColor(t.Text).SingleLine()
		})
}

// editTagChip 是连接弹窗里的可删除标签胶囊：右侧 × 移除标签。
func editTagChip(c *ui.Context, tag connTagDraft, remove func()) {
	t := c.Theme()
	col := tagColor(tag.color)
	ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).
		Padding(1, 5).Radius(t.Space(1)).
		Background(col.Alpha(0.14)).
		Children(func() {
			ui.Text(c, "●").FontSize(8).TextColor(col)
			ui.Text(c, tag.label).FontSize(11).TextColor(t.Text).SingleLine()
			if iconButtonPlain(c, iconClose, "移除标签 "+tag.label, ui.Transparent).Clicked() {
				remove()
			}
		})
}
