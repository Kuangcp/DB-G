package ui

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settingsView 是独立设置窗口的内容：开发用的编辑器性能面板（HUD/预设/高亮策略）。
func (a *root) settingsView(c *ui.Context) {
	disabled := a.activeConsole == ""
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Text(c, "编辑器性能（开发用）").Bold()
		ui.Text(c, fmt.Sprintf(
			"行数=%d 字节=%d 版本=%d",
			a.code.LineCount(), a.code.ByteLen(), a.code.Version(),
		)).FontSize(12).SingleLine()
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "刷新").Clicked() {
				a.updateSettingsWindow()
			}
		})

		ui.Text(c, "预设（写入当前激活控制台并立即保存）").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			for _, n := range []int{3_000, 12_000, 100_000} {
				label := fmt.Sprintf("%dk", n/1_000)
				if ui.Button(c, label).Disabled(disabled).Clicked() {
					a.applyEditorPreset(n)
				}
			}
		})

		ui.Text(c, "补全").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "刷新补全缓存").Clicked() {
				a.refreshCompletions()
			}
		})

		if disabled {
			ui.Text(c, "没有激活的控制台：预设不可用").FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if a.consoleErr != "" {
			ui.Text(c, a.consoleErr).TextColor(c.Theme().Danger).FontSize(12)
		}
	})
}

// applyEditorPreset 把 n 行的演示 SQL 写进当前激活控制台并立即落盘（spike 回归用）。
func (a *root) applyEditorPreset(n int) {
	if a.activeConsole == "" {
		return
	}
	a.setActiveText(buildSQL(n))
	a.syncActiveBuffer()
	_ = a.flushActive()
	a.updateSettingsWindow()
}

// settingsWin 是设置窗口需要的最小能力（便于 headless 测试注入假窗口）。
type settingsWin interface {
	Focus()
	Update(func())
}

// requestSettings 请求打开（或聚焦）设置窗口。
// 有宿主窗口时通过 win.Update 排队到主循环（不在帧构建中途建窗）；
// 没有宿主窗口（headless 测试）时记请求，由帧首 maybeOpenSettings 处理。
func (a *root) requestSettings() {
	if a.win != nil {
		win := a.win
		win.Update(func() { a.openSettings() })
		return
	}
	a.settingsRequested = true
}

// openSettings 打开（或聚焦）设置窗口（单例）。
func (a *root) openSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Focus()
		return
	}
	open := a.openSettingsWin
	if open == nil {
		open = a.newSettingsWindow
	}
	a.settingsWin = open(mygo.WindowOptions{
		Title:   "设置 — db-g",
		Width:   560,
		Height:  340,
		Content: ui.View(a.settingsView),
	})
}

// maybeOpenSettings 消费 headless 的「打开设置」请求。
func (a *root) maybeOpenSettings() {
	if !a.settingsRequested {
		return
	}
	a.settingsRequested = false
	if a.openSettingsWin == nil && a.win == nil {
		return
	}
	a.openSettings()
}

// newSettingsWindow 是生产实现的窗口工厂：建窗并在关闭后清空单例指针。
func (a *root) newSettingsWindow(opts mygo.WindowOptions) settingsWin {
	win := mygo.NewWindow(opts)
	win.OnClose(func(*mygo.CloseEvent) {
		win.Update(func() { a.settingsWin = nil })
	})
	return win
}

// updateSettingsWindow 让设置窗口重画（HUD/策略变化后）。
func (a *root) updateSettingsWindow() {
	if w := a.settingsWin; w != nil {
		w.Update(func() {})
	}
}
