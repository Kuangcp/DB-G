package ui

import "github.com/egoist/mygo/ui"

// iconButton 是只显示图标的小按钮。官方写法：Button 不带文字，Label 给读屏命名，
// Tooltip 说明含义，图标作为子元素（图标取祖先的文字颜色）。放在 ui.Toolbar 里时，
// 它悬停前没有面孔，正好省空间。
func iconButton(c *ui.Context, icon *ui.SVG, label string, col ui.Color) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label)
	b.Children(func() { iconChild(c, icon, col) })
	return b
}

// iconButtonPlain 是没有常驻面孔的小图标按钮，悬停时才浮出浅底：给标签的关闭
// 按钮这类不必一直占视觉重量的地方用。
func iconButtonPlain(c *ui.Context, icon *ui.SVG, label string, col ui.Color) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(t.Space(0.5)).Radius(t.Space(1))
	b.Children(func() { iconChild(c, icon, col) })
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	return b
}

// iconChild 画按钮里的图标；col 全透明时跟随按钮的文字颜色。
func iconChild(c *ui.Context, icon *ui.SVG, col ui.Color) {
	ic := ui.Icon(c, icon).FontSize(16)
	if col.A > 0 {
		ic.TextColor(col)
	}
}
