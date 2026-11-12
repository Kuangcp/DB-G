package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// SetAppIconPNG 由 main 传入打包进程序的图标字节；自举安装时写到图标目录，
// 同时解码一份给标题栏渲染。
func (a *root) SetAppIconPNG(png []byte) {
	a.appIconPNG = png
	if src, err := ui.DecodeImage(png); err == nil {
		a.appIcon = src
	}
}

// installModal 渲染「安装到应用列表 / 升级」确认弹窗。
func (a *root) installModal(c *ui.Context) {
	ui.Modal(c, &a.installOpen, func() {
		title := "安装到应用列表"
		action := "安装"
		switch {
		case a.installNeed.Upgrade:
			title, action = "升级应用", "升级"
		case a.installNeed.Repair:
			title, action = "修复应用菜单", "修复"
		}
		ui.Text(c, title).Bold()
		ui.Text(c, fmt.Sprintf("当前版本 %s，将复制到下面的位置并添加应用菜单项。", a.installNeed.Running)).
			FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		ui.Text(c, a.installNeed.BinPath).Font("monospace").FontSize(11).
			TextColor(c.Theme().TextMuted).SingleLine().Tooltip(a.installNeed.BinPath)
		if a.installNeed.Installed != "" {
			ui.Text(c, "已安装版本 "+a.installNeed.Installed).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		switch {
		case a.installBusy:
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "安装中…").FontSize(12).TextColor(c.Theme().TextMuted)
			})
		case a.installErr != "":
			ui.Text(c, "安装失败："+a.installErr).FontSize(12).
				TextColor(c.Theme().Danger).SingleLine().Tooltip(a.installErr)
		default:
			ui.Text(c, "").FontSize(12)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
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
		a.installNote = fmt.Sprintf("已安装 %s，可从应用菜单启动", st.Running)
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
