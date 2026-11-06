package ui

import "github.com/egoist/mygo/ui"

// sideNavRow 是侧边导航的一行：没有按钮外观，行底一条分隔线，选中项用 accent
// 浅底 + 加粗，悬停浮浅底。连接弹窗的数据源列表和设置窗口的分区导航共用。
func sideNavRow(c *ui.Context, label string, icon *ui.SVG, iconColor ui.Color, selected bool) ui.Element {
	t := c.Theme()
	row := ui.ButtonBase(c).Label(label).Padding(10, 10).Gap(10).
		Justify(ui.Start).AlignItems(ui.Center)
	row.BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(0.12))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if icon != nil {
			ic := ui.Icon(c, icon).FontSize(16)
			if iconColor.A > 0 {
				ic.TextColor(iconColor)
			}
		}
		txt := ui.Text(c, label).SingleLine()
		if selected {
			txt.Bold()
		} else {
			txt.TextColor(t.TextMuted)
		}
	})
	return row
}
