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
	ui.Modal(c, &a.install.Open, func() {
		title := locales.InstallTitle()
		action := locales.InstallAction()
		switch {
		case a.install.Need.Upgrade:
			title, action = locales.InstallUpgradeTitle(), locales.InstallUpgradeAction()
		case a.install.Need.Repair:
			title, action = locales.InstallRepairTitle(), locales.InstallRepairAction()
		}
		ui.Text(c, title).Bold()
		ui.Text(c, locales.InstallBody(a.install.Need.Running)).
			FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		ui.Text(c, a.install.Need.BinPath).Font("monospace").FontSize(11).
			TextColor(c.Theme().TextMuted).SingleLine().Tooltip(a.install.Need.BinPath)
		if a.install.Need.Installed != "" {
			ui.Text(c, locales.InstallInstalledVersion(a.install.Need.Installed)).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		switch {
		case a.install.Busy:
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.InstallRunning()).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		case a.install.Err != "":
			ui.Text(c, locales.InstallFailed(a.install.Err)).FontSize(12).
				TextColor(c.Theme().Danger).SingleLine().Tooltip(a.install.Err)
		default:
			ui.Text(c, "").FontSize(12)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.install.Open = false
			}
			if ui.PrimaryButton(c, action).Disabled(a.install.Busy).Clicked() {
				a.startInstall()
			}
		})
	}).Width(560).Height(240)
}

// startInstall 在后台复制二进制并写入 version / desktop / 图标；headless 同步执行。
func (a *root) startInstall() {
	if a.install.Busy {
		return
	}
	fn := a.install.Install
	if fn == nil {
		fn = app.InstallSelf
	}
	icon := a.appIconPNG
	a.install.Busy = true
	a.install.Err = ""
	run := func() (app.InstallStatus, error) { return fn(icon) }
	done := func(st app.InstallStatus, err error) {
		a.install.Busy = false
		if err != nil {
			a.install.Err = err.Error()
			app.LogError("install", "self install failed", "err", err.Error())
			return
		}
		a.install.Need = st
		a.install.Open = false
		a.install.Note = locales.InstallDone(st.Running)
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
