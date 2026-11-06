package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// tabBar 渲染当前工作区的控制台标签、新建按钮与工作区切换器。
// 标签放进 Toolbar，由它负责「放不下的收进 More 菜单」；+ 与工作区切换器
// 在外层 Row 里，永远可见，不参与标签溢出。
func (a *root) tabBar(c *ui.Context) {
	m := a.consoleMgr()
	var open []app.ConsoleRecord
	if m != nil {
		open = m.Open()
	}
	counts := map[string]int{}
	for _, rec := range open {
		counts[rec.ConnectionID]++
	}
	name := "无工作区"
	if m != nil {
		if ws, ok := m.ActiveWorkspace(); ok {
			name = ws.Name
		}
	}
	ui.Row(c).AlignItems(ui.Center).Gap(6).FillWidth().Children(func() {
		if len(open) == 0 {
			ui.Text(c, "在左侧数据源下新建或打开控制台").TextColor(c.Theme().TextMuted).FontSize(11.5).SingleLine()
			ui.Spacer(c)
		} else {
			ui.Toolbar(c, func() {
				for _, rec := range open {
					a.tab(c, rec, counts[rec.ConnectionID] > 1)
				}
			}).Padding(0).Grow(1).Label("控制台标签")
		}
		ui.Toolbar(c, func() {
			if iconButton(c, iconPlus, "新建控制台", ui.Transparent).Disabled(a.activeConnID() == "").Clicked() {
				a.createConsole()
			}
			ui.MenuButton(c, name, a.workspaceMenu)
		}).Padding(0).Shrink(0)
	})
}

// tab 是一个控制台标签：数据源名在前，控制台名在后且字号更小；
// 当前工作区里这个数据源只打开一个控制台时，省略控制台名。
func (a *root) tab(c *ui.Context, rec app.ConsoleRecord, showConsole bool) {
	m := a.consoleMgr()
	t := c.Theme()
	src := rec.ConnectionID
	if p, ok := a.store.Get(rec.ConnectionID); ok {
		src = p.Name
	}
	dirty := m != nil && m.Dirty(rec.ID)
	label := src
	if showConsole {
		label += " / " + rec.Name
	}
	if dirty {
		label = "● " + label
	}
	active := rec.ID == a.activeConsole
	btn := ui.ButtonBase(c).Label(label).Gap(4).Height(t.Space(5.5)).
		Padding(1, 6).Radius(t.Space(1)).
		BorderWidth(0, 0, 2, 0).BorderColor(ui.Transparent)
	if active {
		btn.BorderColor(t.Accent).Background(t.Accent.Alpha(0.10))
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		if dirty {
			ui.Text(c, "●").FontSize(9).TextColor(t.Accent)
		}
		srcText := ui.Text(c, src).FontSize(11.5).SingleLine()
		if active {
			srcText.Bold()
		}
		if showConsole {
			ui.Text(c, "/").FontSize(9.5).TextColor(t.TextMuted)
			col := t.TextMuted
			if active {
				col = t.Text
			}
			ui.Text(c, rec.Name).FontSize(9.5).TextColor(col).SingleLine()
		}
	})
	if btn.Clicked() {
		a.pendingActivate = rec.ID
	}
	btn.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("关闭").Chosen() {
			a.pendingClose = rec.ID
		}
		if menu.Item("重命名…").Chosen() {
			a.openModal("rename", rec.ID, rec.Name)
		}
		if menu.Item("删除…").Chosen() {
			a.openModal("delete", rec.ID, rec.Name)
		}
	})
}

// createConsole 在当前连接下新建控制台（默认名），下一帧激活。
func (a *root) createConsole() {
	m := a.consoleMgr()
	connID := a.activeConnID()
	if m == nil || connID == "" {
		return
	}
	rec, err := m.Create(connID, "")
	if err != nil {
		a.consoleErr = "新建控制台失败: " + err.Error()
		app.LogWarn("console", "create failed", "err", err.Error())
		return
	}
	app.LogInfo("console", "create", "id", rec.ID, "name", rec.Name)
	a.pendingActivate = rec.ID
}

// closeConsole 把控制台移出当前工作区（文件与元数据保留）。
func (a *root) closeConsole(id string) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	wasActive := a.activeConsole == id
	if err := m.CloseConsole(id); err != nil {
		a.consoleErr = "关闭控制台失败: " + err.Error()
		app.LogWarn("console", "close failed", "id", id, "err", err.Error())
		return
	}
	app.LogInfo("console", "close", "id", id)
	if wasActive {
		a.afterActiveRemoved()
	}
}

// afterActiveRemoved 在当前激活控制台消失后回落到工作区的激活控制台或空态。
func (a *root) afterActiveRemoved() {
	m := a.consoleMgr()
	if m == nil {
		a.clearEditorConsole()
		return
	}
	if rec, ok := m.Active(); ok {
		a.pendingActivate = rec.ID
		return
	}
	a.clearEditorConsole()
}

// clearEditorConsole 进入「无激活控制台」空态。
func (a *root) clearEditorConsole() {
	a.activeConsole = ""
	a.code.Clear()
	a.lastCaret = [2]int{}
}

// workspaceMenu 构建工作区切换菜单。
func (a *root) workspaceMenu(menu *ui.Menu) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	active, hasActive := m.ActiveWorkspace()
	for _, ws := range m.Workspaces() {
		if menu.Item(ws.Name).Checked(hasActive && ws.ID == active.ID).Chosen() {
			a.pendingSwitchWS = ws.ID
		}
	}
	menu.Separator()
	if menu.Item("新建工作区…").Chosen() {
		a.openModal("ws-new", "", "")
	}
	if menu.Item("重命名当前工作区…").Disabled(!hasActive).Chosen() {
		a.openModal("ws-rename", active.ID, active.Name)
	}
	if menu.Item("删除当前工作区…").Disabled(!hasActive).Chosen() {
		a.openModal("ws-delete", active.ID, active.Name)
	}
}

// openModal 打开标签/工作区相关弹窗。
func (a *root) openModal(kind, target, text string) {
	a.modalKind, a.modalTarget, a.modalText, a.modalErr = kind, target, text, ""
}

// tabModal 渲染重命名/删除/工作区弹窗。
func (a *root) tabModal(c *ui.Context) {
	if a.modalKind == "" {
		return
	}
	m := a.consoleMgr()
	if m == nil {
		a.modalKind = ""
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		switch a.modalKind {
		case "rename":
			ui.Text(c, "重命名控制台").Bold()
			ui.TextInput(c, &a.modalText).Placeholder("名称").AutoFocus()
			a.modalButtons(c, func() {
				if strings.TrimSpace(a.modalText) == "" {
					a.modalErr = "名称不能为空"
					return
				}
				if err := m.Rename(a.modalTarget, a.modalText); err != nil {
					a.modalErr = "重命名失败: " + err.Error()
					return
				}
				app.LogInfo("console", "rename", "id", a.modalTarget, "name", a.modalText)
				a.modalKind = ""
			})
		case "delete":
			ui.Text(c, "删除控制台「"+a.modalText+"」？").Bold()
			ui.Text(c, "将同时删除它的 .sql 文件，不可撤销").TextColor(c.Theme().TextMuted)
			a.modalButtons(c, func() {
				wasActive := a.activeConsole == a.modalTarget
				if err := m.DeleteConsole(a.modalTarget); err != nil {
					a.modalErr = "删除失败: " + err.Error()
					app.LogWarn("console", "delete failed", "id", a.modalTarget, "err", err.Error())
					return
				}
				app.LogInfo("console", "delete", "id", a.modalTarget)
				delete(a.consoles, a.modalTarget)
				delete(a.lastVer, a.modalTarget)
				a.code.Drop(a.modalTarget)
				a.modalKind = ""
				if wasActive {
					a.afterActiveRemoved()
				}
			})
		case "ws-new":
			ui.Text(c, "新建工作区").Bold()
			ui.TextInput(c, &a.modalText).Placeholder("名称（留空为「默认」）").AutoFocus()
			a.modalButtons(c, func() {
				ws, err := m.CreateWorkspace(a.modalText)
				if err != nil {
					a.modalErr = "新建工作区失败: " + err.Error()
					return
				}
				a.modalKind = ""
				a.clearEditorConsole()
				_ = ws
			})
		case "ws-rename":
			ui.Text(c, "重命名工作区").Bold()
			ui.TextInput(c, &a.modalText).Placeholder("名称").AutoFocus()
			a.modalButtons(c, func() {
				if err := m.RenameWorkspace(a.modalTarget, a.modalText); err != nil {
					a.modalErr = "重命名失败: " + err.Error()
					return
				}
				a.modalKind = ""
			})
		case "ws-delete":
			ui.Text(c, "删除工作区「"+a.modalText+"」？").Bold()
			ui.Text(c, "控制台与其 .sql 文件都会保留，可从侧栏重新打开").TextColor(c.Theme().TextMuted)
			a.modalButtons(c, func() {
				if err := m.DeleteWorkspace(a.modalTarget); err != nil {
					a.modalErr = "删除失败: " + err.Error()
					return
				}
				a.modalKind = ""
				if rec, ok := m.Active(); ok {
					a.pendingActivate = rec.ID
				} else {
					a.clearEditorConsole()
				}
			})
		}
		if a.modalErr != "" {
			ui.Text(c, a.modalErr).TextColor(c.Theme().Danger)
		}
	})
	if !open {
		a.modalKind = ""
	}
}

// modalButtons 渲染「取消 / 保存」两个按钮，保存时执行 save。
func (a *root) modalButtons(c *ui.Context, save func()) {
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "取消").Clicked() {
			a.modalKind = ""
		}
		if ui.PrimaryButton(c, "保存").Clicked() {
			save()
		}
	})
}
