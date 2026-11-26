package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// SetAppIconPNG 由 main 传入打包进程序的图标字节；自举安装时写到图标目录。
func (a *root) SetAppIconPNG(png []byte) {
	a.appIconPNG = png
}

// installModal 渲染「安装到应用列表 / 升级」确认弹窗。
func (a *root) installModal(c *ui.Context) {
	ui.Modal(c, &a.installOpen, func() {
		title := locales.InstallTitle()
		action := locales.InstallAction()
		switch {
		case a.installNeed.Upgrade:
			title, action = locales.InstallUpgradeTitle(), locales.InstallUpgradeAction()
		case a.installNeed.Repair:
			title, action = locales.InstallRepairTitle(), locales.InstallRepairAction()
		}
		ui.Text(c, title).Bold()
		ui.Text(c, locales.InstallBody(a.installNeed.Running)).
			FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		ui.Text(c, a.installNeed.BinPath).Font("monospace").FontSize(11).
			TextColor(c.Theme().TextMuted).SingleLine().Tooltip(a.installNeed.BinPath)
		if a.installNeed.Installed != "" {
			ui.Text(c, locales.InstallInstalledVersion(a.installNeed.Installed)).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		switch {
		case a.installBusy:
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.InstallRunning()).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		case a.installErr != "":
			ui.Text(c, locales.InstallFailed(a.installErr)).FontSize(12).
				TextColor(c.Theme().Danger).SingleLine().Tooltip(a.installErr)
		default:
			ui.Text(c, "").FontSize(12)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.installOpen = false
			}
			if ui.PrimaryButton(c, action).Disabled(a.installBusy).Clicked() {
				a.startInstall()
			}
		})
	}).Width(560).Height(240)
}

// startInstall 在后台复制二进制并写入 version / desktop / 图标；headless 同步执行。
func (a *root) startInstall() {
	if a.installBusy {
		return
	}
	fn := a.installFn
	if fn == nil {
		fn = app.InstallSelf
	}
	icon := a.appIconPNG
	a.installBusy = true
	a.installErr = ""
	run := func() (app.InstallStatus, error) { return fn(icon) }
	done := func(st app.InstallStatus, err error) {
		a.installBusy = false
		if err != nil {
			a.installErr = err.Error()
			app.LogError("install", "self install failed", "err", err.Error())
			return
		}
		a.installNeed = st
		a.installOpen = false
		a.installNote = locales.InstallDone(st.Running)
		app.LogInfo("install", "self install", "bin", st.BinPath, "version", st.Installed)
	}
	if a.win == nil {
		st, err := run()
		done(st, err)
		return
	}
	win := a.win
	go func() {
		st, err := run()
		a.safeUpdate(win, func() { done(st, err) })
	}()
}
