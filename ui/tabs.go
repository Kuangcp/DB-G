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
			ui.Text(c, "在左侧数据源下新建或打开控制台").TextColor(c.Theme().TextMuted).FontSize(12.5).SingleLine()
			ui.Spacer(c)
		} else {
			ui.Toolbar(c, func() {
				for _, rec := range open {
					a.tab(c, rec, counts[rec.ConnectionID] > 1)
				}
			}).Padding(0).Grow(1).Label("控制台标签")
		}
		// 新建控制台只从左侧数据源下的入口走，这里只留工作区切换器。
		ui.Toolbar(c, func() {
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
	warn := false
	if st, ok := a.extFiles[rec.ID]; ok && (st.conflict || st.missing) {
		warn = true
	}
	label := src
	if showConsole {
		label += " / " + rec.Name
	}
	if warn {
		label = "⚠ " + label
	}
	if dirty {
		label = "● " + label
	}
	active := rec.ID == a.activeConsole
	btn := ui.ButtonBase(c).Label(label).Gap(5).Height(t.Space(6.5)).
		Padding(2, 9).Radius(t.Space(1.25)).
		BorderWidth(0, 0, 2, 0).BorderColor(ui.Transparent)
	if active {
		btn.BorderColor(t.Accent).Background(t.Accent.Alpha(0.10))
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		if warn {
			ui.Text(c, "⚠").FontSize(10).TextColor(t.Danger).
				Tooltip("外部文件已修改或缺失，自动保存已暂停")
		}
		if dirty {
			ui.Text(c, "●").FontSize(10).TextColor(t.Accent)
		}
		srcText := ui.Text(c, src).FontSize(12.5).SingleLine()
		if active {
			srcText.Bold()
		}
		if showConsole {
			ui.Text(c, "/").FontSize(10.5).TextColor(t.TextMuted)
			col := t.TextMuted
			if active {
				col = t.Text
			}
			ui.Text(c, rec.Name).FontSize(10.5).TextColor(col).SingleLine()
		}
	})
	if btn.Clicked() {
		a.pendingActivate = rec.ID
	}
	attachTabDrag(c, btn, rec.ID)
	a.attachTabDrop(c, btn, rec)
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

// tabDrag 是标签拖拽的值：控制台 id。
type tabDrag struct{ id string }

// attachTabDrag 只在按住 Ctrl 时把标签接成拖拽源，避免普通点击误拖（与侧栏树一致）。
func attachTabDrag(c *ui.Context, btn ui.Element, id string) {
	held := c.Modifiers()&ui.Ctrl != 0
	if !held && !btn.Dragging() {
		return
	}
	btn.Drag(tabDrag{id: id})
	if held {
		btn.Cursor(ui.CursorGrab).Tooltip("按住 Ctrl 拖动：调整控制台顺序")
	}
	if btn.Dragging() {
		btn.Opacity(0.45)
	}
}

// tabDropSpot 根据指针在标签内的横向位置决定插到前面还是后面。
func tabDropSpot(btn ui.Element, drag tabDrag, target app.ConsoleRecord) (dropSpot, bool) {
	if drag.id == target.ID {
		return dropNone, false
	}
	x, _, ok := btn.PointerPosition()
	if !ok {
		return dropNone, false
	}
	if x < btn.Bounds().W/2 {
		return dropBefore, true
	}
	return dropAfter, true
}

// attachTabDrop 把标签接成拖拽目标：在左右边缘画插入线。
func (a *root) attachTabDrop(c *ui.Context, btn ui.Element, target app.ConsoleRecord) {
	if drag, over := ui.DragOver[tabDrag](btn); over {
		if spot, ok := tabDropSpot(btn, drag, target); ok {
			styleTabDrop(btn, spot, c.Theme())
		}
	}
	if drag, ok := ui.Drop[tabDrag](btn); ok {
		if spot, ok := tabDropSpot(btn, drag, target); ok {
			a.applyTabDrop(drag, target, spot)
		}
	}
}

func styleTabDrop(btn ui.Element, spot dropSpot, t *ui.Theme) {
	btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
		x := r.X
		if spot == dropAfter {
			x = r.X + r.W - 2
		}
		p.Fill(ui.Rect{X: x, Y: r.Y, W: 2, H: r.H}, t.Accent, 1)
	})
}

// applyTabDrop 把拖动的控制台移到目标前/后。
func (a *root) applyTabDrop(drag tabDrag, target app.ConsoleRecord, spot dropSpot) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	ws, ok := m.ActiveWorkspace()
	if !ok {
		return
	}
	before := ""
	if spot == dropBefore {
		before = target.ID
	} else {
		before = nextWorkspaceMember(ws, target.ID)
	}
	if err := m.Store().MoveWorkspaceMember(ws.ID, drag.id, before); err != nil {
		app.LogWarn("console", "tab move failed", "id", drag.id, "before", before, "err", err.Error())
		return
	}
	app.LogInfo("console", "tab moved", "id", drag.id, "before", before)
}

// nextWorkspaceMember 返回成员里的下一个控制台 id；末尾返回空串。
func nextWorkspaceMember(ws app.WorkspaceRecord, id string) string {
	for i, member := range ws.Members {
		if member == id && i+1 < len(ws.Members) {
			return ws.Members[i+1]
		}
	}
	return ""
}

// touchConsoleMRU 把控制台置顶到最近使用列表（Ctrl+Tab 用）；列表封顶 16 条。
func (a *root) touchConsoleMRU(id string) {
	kept := make([]string, 0, len(a.consoleMRU)+1)
	kept = append(kept, id)
	for _, v := range a.consoleMRU {
		if v != id {
			kept = append(kept, v)
		}
	}
	if len(kept) > 16 {
		kept = kept[:16]
	}
	a.consoleMRU = kept
}

// switchToRecentConsole 切到当前工作区里最近使用过的那个控制台；
// 连续按就在最近两个之间来回切（Alt-Tab 式）。
func (a *root) switchToRecentConsole() {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	open := make(map[string]bool)
	for _, rec := range m.Open() {
		open[rec.ID] = true
	}
	for _, id := range a.consoleMRU {
		if id != a.activeConsole && open[id] {
			a.pendingActivate = id
			return
		}
	}
}

// closeConsole 把控制台移出当前工作区（文件与元数据保留）。
func (a *root) closeConsole(id string) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if a.externalHasLocalChanges(id) {
		a.pendingActivate = id
		a.extResolveOpen = true
		a.extMsg = "该文件有未解决的外部修改，请先处理"
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
	a.saveStmtRuns(a.activeConsole)
	a.stmtRuns, a.activeStmt = nil, 0
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
			hint := "将同时删除它的 .sql 文件，不可撤销"
			if rec, ok := a.store.GetConsole(a.modalTarget); ok && rec.External {
				hint = "只解除绑定，磁盘上的原始 .sql 文件会保留"
			}
			ui.Text(c, hint).TextColor(c.Theme().TextMuted)
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
				a.removeExternalState(a.modalTarget)
				a.dropStmtRunsOf([]string{a.modalTarget})
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
