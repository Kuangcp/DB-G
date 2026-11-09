package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// connTagDraft 是连接弹窗里一个标签槽的编辑态。
type connTagDraft struct {
	label string
	color string
}

// resetConnTagForm 清空三个标签槽并给每个槽默认颜色。
func (a *root) resetConnTagForm() {
	for i := range a.connFormTags {
		a.connFormTags[i] = connTagDraft{color: defaultTagColor(i)}
	}
}

// setConnTagForm 用档案里的标签填充编辑槽；不足三格的补默认颜色。
func (a *root) setConnTagForm(tags []app.ConnTag) {
	a.resetConnTagForm()
	for i, tag := range tags {
		if i >= len(a.connFormTags) {
			break
		}
		a.connFormTags[i] = connTagDraft{label: tag.Label, color: tag.Color}
	}
}

// connTagFormList 把编辑槽读成数据层标签，空槽自动丢弃。
func (a *root) connTagFormList() []app.ConnTag {
	tags := make([]app.ConnTag, 0, len(a.connFormTags))
	for _, tag := range a.connFormTags {
		if label := strings.TrimSpace(tag.label); label != "" {
			tags = append(tags, app.ConnTag{Label: label, Color: tag.color})
		}
	}
	return app.NormalizeTags(tags)
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
