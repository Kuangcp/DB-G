package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// floatingEditorTools 把查找/格式化/AI 补全悬浮在编辑区右上角，省掉一整行工具条。
// 半透明底让下面的代码不至于糊在图标上；平时没有按钮面孔，悬停才浮出浅底。
func (a *root) floatingEditorTools(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Absolute().Top(t.Space(1)).Right(t.Space(3)).Gap(2).AlignItems(ui.Center).
		Padding(2, 3).Radius(t.Space(1.5)).Background(t.Background.Alpha(0.92)).
		Children(func() {
			if iconButtonPlain(c, iconSearch, locales.TopFind(), t.TextMuted).Clicked() {
				a.code.Find()
			}
			if iconButtonPlain(c, iconFormat, locales.KeyFormat(), t.TextMuted).Clicked() {
				a.formatActive()
			}
			if iconButtonPlain(c, iconSparkles, locales.TopAIComplete(), t.TextMuted).Clicked() {
				a.requestAICompletion()
			}
		})
}
