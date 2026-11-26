package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// editorToolButtons 是查找/格式化/AI 补全三个图标，放在顶栏下第二行的最右侧。
func (a *root) editorToolButtons(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
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
