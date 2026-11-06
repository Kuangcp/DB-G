package ui

import "github.com/egoist/mygo/ui"

// keymapSection 是设置窗口里的「快捷键」分区：每个动作一行，点「录制」后
// 按新的组合即可改绑（Esc 取消），也可以单项/全部恢复默认。
func (a *root) keymapSection(c *ui.Context) {
	ui.Text(c, "快捷键").FontSize(12).TextColor(c.Theme().TextMuted)
	if hint := keymapHint(a); hint != "" {
		// 录制框：拿焦点接住任意键（HandleInput 收到的是控制键与特殊键，
		// 普通字母由它下面的输入路径来，这里只处理按键）。
		ui.Text(c, hint).FontSize(12).TextColor(c.Theme().Accent).SingleLine()
		// Focus（不是 AutoFocus）：录制期间每帧都把焦点钉在框上，
		// 这样按什么键都先到这里，不会被下面的按钮抢走。
		ui.Box(c).FillWidth().Height(20).
			Focusable().Focus().
			HandleInput(a.captureKey)
	}
	if a.keymapWarn != "" {
		ui.Text(c, a.keymapWarn).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
	for _, act := range keyActions {
		recording := a.captureAction == act.ID
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			label := ui.Text(c, act.Label).FontSize(12).SingleLine()
			if recording {
				label.TextColor(c.Theme().Accent)
			}
			ui.Spacer(c)
			keyText := a.chordText(act.ID)
			col := c.Theme().TextMuted
			if recording {
				keyText, col = "按下新组合…", c.Theme().Accent
			}
			ui.Text(c, keyText).FontSize(12).TextColor(col).SingleLine()
			if ui.Button(c, "录制").Disabled(recording).Clicked() {
				a.captureAction = act.ID
			}
			if ui.Button(c, "恢复默认").Disabled(chordsEqual(a.chords[act.ID], act.Def)).Clicked() {
				a.resetChords(act.ID)
			}
		})
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "全部恢复默认").Clicked() {
			a.resetChords("")
			a.captureAction = ""
		}
		if a.captureAction != "" {
			if ui.Button(c, "取消录制").Clicked() {
				a.captureAction = ""
			}
		}
	})
}
