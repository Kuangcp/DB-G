package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// consoleTabs 渲染当前工作区的控制台标签，现在放在顶部标题栏里。
// 标签放进 Toolbar，放不下的收进 More 菜单。
func (a *root) consoleTabs(c *ui.Context) {
	m := a.consoleMgr()
	var open []app.ConsoleRecord
	if m != nil {
		open = m.Open()
	}
	counts := map[string]int{}
	for _, rec := range open {
		counts[rec.ConnectionID]++
	}
	if len(open) == 0 && len(a.viewerOrder) == 0 {
		ui.Text(c, locales.TabEmptyHint()).TextColor(c.Theme().TextMuted).FontSize(a.tabFontSize()).SingleLine()
		ui.Spacer(c)
		return
	}
	ui.Toolbar(c, func() {
		first := true
		for _, rec := range open {
			a.tab(c, rec, counts[rec.ConnectionID] > 1, first)
			first = false
		}
		// 表视图是临时标签，排在控制台之后。
		for _, id := range a.viewerOrder {
			if t := a.viewerTabs[id]; t != nil {
				a.viewerTabButton(c, t, first)
				first = false
			}
		}
	}).Padding(0).Grow(1).Label(locales.TabToolbarLabel())
}

// viewerTabButton 渲染一个表/视图浏览标签（图标 + 对象名，关闭/刷新走右键菜单）。
func (a *root) viewerTabButton(c *ui.Context, t *viewerTab, first bool) {
	th := c.Theme()
	size := a.tabFontSize()
	height := max(th.Space(6.5), size*1.7+4)
	active := a.activeViewer == t.id
	btn := ui.ButtonBase(c).Label(t.obj.Name).Gap(5).Height(height).Padding(2, 9)
	if !first {
		btn.BorderWidth(0, 0, 0, 1).BorderColor(th.Border)
	}
	if active {
		btn.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, th.Accent, 0)
		})
	} else if btn.Hovered() {
		btn.Background(th.SurfaceHover)
	}
	label := t.obj.Name
	btn.Children(func() {
		if icon := objectKindIcon(t.obj.Kind); icon != nil {
			ui.Icon(c, icon).FontSize(size).TextColor(th.TextMuted)
		}
		txt := ui.Text(c, t.obj.Name).FontSize(size).SingleLine()
		if active {
			txt.Bold()
		}
	})
	btn.Tooltip(locales.ViewerTabTooltip(a.connName(t.connID), label))
	if btn.Clicked() {
		a.pendingActivateViewer = t.id
	}
	btn.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.ViewerRefresh()).Chosen() {
			if es := a.editSets[viewerRuntimeID(t.id)]; es != nil && es.Count() > 0 {
				a.toastError(locales.PageDirty())
			} else {
				a.viewerReload(t, nil)
			}
		}
		if menu.Item(locales.TabMenuClose()).Chosen() {
			a.closeViewerTab(t.id)
		}
	})
}

// tabsRow 是标题栏下的控制台标签行（标签之间用竖线分隔）。
func (a *root) tabsRow(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).FillWidth().Padding(0, 8).Children(func() {
		a.consoleTabs(c)
	})
}

// tabFontSize 返回控制台标签的字号；配置越界时回退默认值。
func (a *root) tabFontSize() float32 {
	size := a.fontPrefs.TabSize
	if size < app.MinTabFontSize || size > app.MaxTabFontSize {
		size = app.DefaultTabFontSize
	}
	return float32(size)
}

// tab 是一个控制台标签：数据源名在前，控制台名在后且字号更小；
// 当前工作区里这个数据源只打开一个控制台时，省略控制台名。
// first 为真时不画左侧分隔线（行里第一个标签）。
func (a *root) tab(c *ui.Context, rec app.ConsoleRecord, showConsole, first bool) {
	m := a.consoleMgr()
	t := c.Theme()
	src := rec.ConnectionID
	kind := ""
	if p, ok := a.store.Get(rec.ConnectionID); ok {
		src = p.Name
		kind = p.Kind
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
	active := rec.ID == a.activeConsole && !a.viewerActive()
	size := a.tabFontSize()
	height := max(t.Space(6.5), size*1.7+4)
	// 简约标签：无边框无底色，标签之间一条竖分隔线（第一个没有）。
	// 激活标签用 2px accent 上边线；上边线画出来而不是边框，切换时尺寸与文字位置不动。
	btn := ui.ButtonBase(c).Label(label).Gap(5).Height(height).Padding(2, 9)
	if !first {
		btn.BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
	}
	if active {
		btn.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, t.Accent, 0)
		})
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		if warn {
			ui.Text(c, "⚠").FontSize(10).TextColor(t.Danger).
				Tooltip(locales.TabExtWarnTooltip())
		}
		if dirty {
			ui.Text(c, "●").FontSize(10).TextColor(t.Accent)
		}
		if icon := connKindIcon(kind); icon != nil {
			ui.Icon(c, icon).FontSize(size).TextColor(connKindColor(kind))
		}
		srcText := ui.Text(c, src).FontSize(size).SingleLine()
		if active {
			srcText.Bold()
		}
		if showConsole {
			sub := max(float32(10), size-2)
			ui.Text(c, "/").FontSize(sub).TextColor(t.TextMuted)
			col := t.TextMuted
			if active {
				col = t.Text
			}
			ui.Text(c, rec.Name).FontSize(sub).TextColor(col).SingleLine()
		}
	})
	if btn.Clicked() {
		a.pendingActivate = rec.ID
	}
	attachTabDrag(c, btn, rec.ID)
	a.attachTabDrop(c, btn, rec)
	btn.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.TabMenuClose()).Chosen() {
			a.pendingClose = rec.ID
		}
		if menu.Item(locales.TabMenuRename()).Chosen() {
			a.openModal("rename", rec.ID, rec.Name)
		}
		if menu.Item(locales.TabMenuDelete()).Chosen() {
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
		btn.Cursor(ui.CursorGrab).Tooltip(locales.TabDragTooltip())
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

// switchToRecentConsole 切到最近使用过的那个标签（控制台或表视图）；
// 连续按就在最近两个之间来回切（Alt-Tab 式）。
func (a *root) switchToRecentConsole() {
	m := a.consoleMgr()
	open := map[string]bool{}
	if m != nil {
		for _, rec := range m.Open() {
			open[rec.ID] = true
		}
	}
	current := a.activeTabKey()
	for _, key := range a.consoleMRU {
		if key == current {
			continue
		}
		if id, ok := strings.CutPrefix(key, "vw:"); ok {
			if _, exists := a.viewerTabs[id]; exists {
				a.pendingActivateViewer = id
				return
			}
			continue
		}
		if open[key] {
			a.pendingActivate = key
			return
		}
	}
}

// activeTabKey 返回当前标签在最近使用序列里的键。
func (a *root) activeTabKey() string {
	if a.viewerActive() {
		return viewerRuntimeID(a.activeViewer)
	}
	return a.activeConsole
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
		a.extMsg = locales.TabExtUnresolved()
		return
	}
	wasActive := a.activeConsole == id
	if err := m.CloseConsole(id); err != nil {
		a.consoleErr = locales.TabCloseFailed(err.Error())
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
	a.refreshStmtMarks()
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
	if menu.Item(locales.WsMenuNew()).Chosen() {
		a.openModal("ws-new", "", "")
	}
	if menu.Item(locales.WsMenuRenameCurrent()).Disabled(!hasActive).Chosen() {
		a.openModal("ws-rename", active.ID, active.Name)
	}
	if menu.Item(locales.WsMenuDeleteCurrent()).Disabled(!hasActive).Chosen() {
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
			ui.Text(c, locales.ModalRenameConsole()).Bold()
			ui.TextInput(c, &a.modalText).Placeholder(locales.ModalName()).AutoFocus()
			a.modalButtons(c, func() {
				if strings.TrimSpace(a.modalText) == "" {
					a.modalErr = locales.ModalNameEmpty()
					return
				}
				if err := m.Rename(a.modalTarget, a.modalText); err != nil {
					a.modalErr = locales.ModalRenameFailed(err.Error())
					return
				}
				app.LogInfo("console", "rename", "id", a.modalTarget, "name", a.modalText)
				a.modalKind = ""
			})
		case "delete":
			ui.Text(c, locales.ModalDeleteConsole(a.modalText)).Bold()
			hint := locales.ModalDeleteConsoleHint()
			if rec, ok := a.store.GetConsole(a.modalTarget); ok && rec.External {
				hint = locales.ModalUnlinkConsoleHint()
			}
			ui.Text(c, hint).TextColor(c.Theme().TextMuted)
			a.modalButtons(c, func() {
				wasActive := a.activeConsole == a.modalTarget
				if err := m.DeleteConsole(a.modalTarget); err != nil {
					a.modalErr = locales.ModalDeleteFailed(err.Error())
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
			ui.Text(c, locales.ModalNewWorkspace()).Bold()
			ui.TextInput(c, &a.modalText).Placeholder(locales.ModalWorkspaceNamePlaceholder()).AutoFocus()
			a.modalButtons(c, func() {
				ws, err := m.CreateWorkspace(a.modalText)
				if err != nil {
					a.modalErr = locales.ModalNewWorkspaceFailed(err.Error())
					return
				}
				a.modalKind = ""
				a.clearEditorConsole()
				_ = ws
			})
		case "ws-rename":
			ui.Text(c, locales.ModalRenameWorkspace()).Bold()
			ui.TextInput(c, &a.modalText).Placeholder(locales.ModalName()).AutoFocus()
			a.modalButtons(c, func() {
				if err := m.RenameWorkspace(a.modalTarget, a.modalText); err != nil {
					a.modalErr = locales.ModalRenameFailed(err.Error())
					return
				}
				a.modalKind = ""
			})
		case "ws-delete":
			ui.Text(c, locales.ModalDeleteWorkspace(a.modalText)).Bold()
			ui.Text(c, locales.ModalDeleteWorkspaceHint()).TextColor(c.Theme().TextMuted)
			a.modalButtons(c, func() {
				if err := m.DeleteWorkspace(a.modalTarget); err != nil {
					a.modalErr = locales.ModalDeleteFailed(err.Error())
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
		if ui.Button(c, locales.CommonCancel()).Clicked() {
			a.modalKind = ""
		}
		if ui.PrimaryButton(c, locales.CommonSave()).Clicked() {
			save()
		}
	})
}
