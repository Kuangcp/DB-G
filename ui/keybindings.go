package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// keymapSection 是设置窗口里的「快捷键」分区：每个动作一行，点「录制」后
// 按新的组合即可改绑（Esc 取消），也可以单项/全部恢复默认。
func (a *root) keymapSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, locales.KeymapTitle()).FontSize(12).TextColor(c.Theme().TextMuted)
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
	if n, ok := a.settingsNoticeOf("keymap"); ok {
		ui.Text(c, n.text).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
	for _, act := range keyActions {
		recording := a.settings.CaptureAction == act.ID
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			label := ui.Text(c, act.Label()).FontSize(12).SingleLine()
			if recording {
				label.TextColor(c.Theme().Accent)
			}
			ui.Spacer(c)
			keyText := a.chordText(act.ID)
			col := c.Theme().TextMuted
			if recording {
				keyText, col = locales.KeymapPressNew(), c.Theme().Accent
			}
			ui.Text(c, keyText).FontSize(12).TextColor(col).SingleLine()
			if ui.Button(c, locales.KeymapRecord()).Radius(cr).Disabled(recording).Clicked() {
				a.settings.CaptureAction = act.ID
			}
			if ui.Button(c, locales.KeymapReset()).Radius(cr).Disabled(chordsEqual(a.settings.Chords[act.ID], act.Def)).Clicked() {
				a.resetChords(act.ID)
			}
		})
	}
	ui.Text(c, locales.KeymapMouseTitle()).FontSize(12).TextColor(c.Theme().TextMuted)
	for _, act := range mouseActions {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, act.Label()).FontSize(12).SingleLine()
			ui.Spacer(c)
			ui.MenuButton(c, mouseGestureText(a.settings.MouseBindings[act.ID]), func(menu *ui.Menu) {
				for _, mods := range mouseGestureOptions() {
					if menu.Item(mouseGestureText(mods)).
						Checked(a.settings.MouseBindings[act.ID] == mods).Chosen() {
						a.setMouseBinding(act.ID, mods)
					}
				}
			})
			if ui.Button(c, locales.KeymapReset()).Radius(cr).
				Disabled(a.settings.MouseBindings[act.ID] == act.Def).Clicked() {
				a.resetMouseBinding(act.ID)
			}
		})
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, locales.KeymapResetAll()).Radius(cr).Clicked() {
			a.resetChords("")
			for _, act := range mouseActions {
				a.resetMouseBinding(act.ID)
			}
			a.settings.CaptureAction = ""
		}
		if a.settings.CaptureAction != "" {
			if ui.Button(c, locales.KeymapCancelRecord()).Radius(cr).Clicked() {
				a.settings.CaptureAction = ""
			}
		}
	})
}
