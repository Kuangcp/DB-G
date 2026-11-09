package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// extFile 是一个外部 .sql 控制台的磁盘一致性状态。
type extFile struct {
	path     string
	lastDisk string    // 最近一次与磁盘对齐的内容
	mod      time.Time // 最近一次看到的 mtime
	size     int64
	known    bool // 是否已建立基线（首次打开/重启后）
	missing  bool // 文件被删除
	conflict bool // 本地有未保存改动，磁盘又被外部修改
}

// externalState 取/建某控制台的外部文件状态。
func (a *root) externalState(id string) *extFile {
	if a.extFiles == nil {
		a.extFiles = map[string]*extFile{}
	}
	st, ok := a.extFiles[id]
	if !ok {
		st = &extFile{}
		a.extFiles[id] = st
	}
	return st
}

// externalRecord 返回控制台记录（仅外部文件）。
func (a *root) externalRecord(id string) (app.ConsoleRecord, bool) {
	if a.store == nil || id == "" {
		return app.ConsoleRecord{}, false
	}
	rec, ok := a.store.GetConsole(id)
	return rec, ok && rec.External
}

// activeExternal 返回当前活动控制台的外部文件状态。
func (a *root) activeExternal() (app.ConsoleRecord, *extFile, bool) {
	rec, ok := a.externalRecord(a.activeConsole)
	if !ok {
		return app.ConsoleRecord{}, nil, false
	}
	return rec, a.externalState(rec.ID), true
}

// localText 返回控制台当前本地文本：活动控制台以编辑器为准，其余用管理器缓存。
func (a *root) localText(id string) string {
	if id == a.activeConsole {
		return a.code.Text()
	}
	if m := a.consoleMgr(); m != nil {
		if text, ok := m.Text(id); ok {
			return text
		}
	}
	return ""
}

// initExternal 建立磁盘基线；后续 pollExternalFiles 只比较 mtime/size。
func (a *root) initExternal(id, path string) {
	st := a.externalState(id)
	st.path = path
	st.known = false
	st.conflict = false
	st.missing = false
	fs := app.StatExternalSQL(path)
	if !fs.Exists {
		st.missing = true
		return
	}
	text, err := app.ReadExternalSQL(path)
	if err != nil {
		st.missing = true
		return
	}
	st.lastDisk = text
	st.mod, st.size = fs.ModTime, fs.Size
	st.known = true
}

// ensureExternal 检查一个外部控制台的磁盘状态：
// 无变化直接返回；变干净→自动重载；本地脏→标冲突并暂停写盘；文件消失→标缺失。
func (a *root) ensureExternal(id string) {
	rec, ok := a.externalRecord(id)
	if !ok {
		return
	}
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if m.Writing(id) {
		// 自己正在写盘：等写完再判断，避免把临时文件/rename 过程当成外部修改。
		return
	}
	st := a.externalState(id)
	st.path = rec.FilePath
	fs := app.StatExternalSQL(rec.FilePath)
	if !fs.Exists {
		if !st.missing {
			st.missing = true
			st.conflict = false
			m.SetSavePaused(id, true)
			app.LogWarn("external", "file missing", "console", id, "path", rec.FilePath)
		}
		return
	}
	st.missing = false
	if st.known && fs.ModTime.Equal(st.mod) && fs.Size == st.size {
		return
	}
	text, err := app.ReadExternalSQL(rec.FilePath)
	if err != nil {
		st.missing = true
		m.SetSavePaused(id, true)
		return
	}
	st.mod, st.size = fs.ModTime, fs.Size
	if !st.known {
		st.known = true
		st.lastDisk = text
		return
	}
	if st.lastDisk == text {
		return
	}
	if saved, ok := m.LastSaved(id); ok && saved == text {
		// 磁盘上的这版就是应用刚自动保存下去的：更新基线，不算外部修改。
		// 必须比对 lastSaved 而不是 localText——用户可能已经在保存之后又改了正文。
		st.lastDisk = text
		st.conflict = false
		m.SetSavePaused(id, false)
		return
	}
	if a.localText(id) == text {
		// 编辑器正文已与磁盘一致（手动保存/外部恰好改成同一内容）：只对齐基线。
		st.lastDisk = text
		st.conflict = false
		m.SetSavePaused(id, false)
		return
	}
	if m.Dirty(id) {
		if !st.conflict {
			st.conflict = true
			m.SetSavePaused(id, true)
			app.LogWarn("external", "conflict", "console", id, "path", rec.FilePath)
		}
		return
	}
	st.lastDisk = text
	st.conflict = false
	m.SetExternalText(id, text)
	m.SetSavePaused(id, false)
	app.LogInfo("external", "reloaded from disk", "console", id, "path", rec.FilePath)
	if id == a.activeConsole {
		a.code.SetActive(id, text)
		a.lastVer[id] = a.code.Version()
	} else {
		a.code.Reload(id, text)
	}
}

// pollExternalFiles 每秒扫一次已打开的外部控制台；force 供测试/显式检查用。
func (a *root) pollExternalFiles(force bool) {
	if a.store == nil || a.mgr == nil {
		return
	}
	if !a.extPollAt.IsZero() && !force && time.Since(a.extPollAt) < time.Second {
		return
	}
	a.extPollAt = time.Now()
	for _, rec := range a.mgr.Open() {
		if rec.External {
			a.ensureExternal(rec.ID)
		}
	}
}

// pruneMissingExternal 启动时把文件已丢失的外部控制台移出所有工作区（保留元数据）。
func (a *root) pruneMissingExternal() {
	if a.store == nil {
		return
	}
	for _, p := range a.store.List() {
		for _, rec := range a.store.ListConsoles(p.ID) {
			if rec.External && !app.StatExternalSQL(rec.FilePath).Exists {
				_ = a.store.CloseConsoleEverywhere(rec.ID)
			}
		}
	}
}

// handleDroppedFiles 处理拖入的 .sql：已绑定路径直接激活，其余弹数据源选择。
func (a *root) handleDroppedFiles(paths []string) {
	if a.store == nil || a.consoleMgr() == nil {
		return
	}
	seen := map[string]bool{}
	var activate []app.ConsoleRecord
	var pending []string
	for _, raw := range paths {
		norm, err := app.CheckExternalSQL(raw)
		if err != nil {
			a.extMsg = filepath.Base(raw) + "：" + err.Error()
			continue
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if rec, ok := a.store.GetConsoleByFilePath(norm); ok {
			activate = append(activate, rec)
			continue
		}
		pending = append(pending, norm)
	}
	for _, rec := range activate {
		a.activateExternalRecord(rec)
	}
	if len(pending) > 0 {
		// 文件管理器拖入后窗口可能不在最前：先把主窗口带到前台，方便直接选数据源。
		if a.win != nil {
			a.win.Focus()
		}
		a.extPickPaths = pending
		a.extPickOpen = true
	}
}

// pickOpenSQLFiles 弹文件选择框（顶栏入口），复用拖入流程。
func (a *root) pickOpenSQLFiles() {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "打开 SQL 文件",
			Filters: []mygo.FileFilter{
				{Name: "SQL", Extensions: []string{"sql"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { a.handleDroppedFiles(paths) })
	}()
}

// addExternalMember 把外部控制台加入当前工作区（不激活）。
func (a *root) addExternalMember(id string) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if ws, err := m.EnsureWorkspace(); err == nil {
		_ = a.store.AddWorkspaceMember(ws.ID, id)
	}
}

// activateExternalRecord 打开已存在的绑定：加入当前工作区并激活。
func (a *root) activateExternalRecord(rec app.ConsoleRecord) {
	if rec.External {
		a.initExternal(rec.ID, rec.FilePath)
	}
	if !a.activateConsole(rec.ID) {
		a.extMsg = "打开控制台失败：" + rec.Name
	}
}

// bindExternal 把待打开的文件绑定到数据源并逐个打开。
func (a *root) bindExternal(connID string) {
	if a.store == nil {
		return
	}
	var last string
	for _, path := range a.extPickPaths {
		if rec, ok := a.store.GetConsoleByFilePath(path); ok {
			last = rec.ID
			continue
		}
		rec, err := a.store.CreateExternalConsole(connID, filepath.Base(path), path)
		if err != nil {
			a.extMsg = "绑定失败：" + err.Error()
			continue
		}
		a.initExternal(rec.ID, path)
		a.addExternalMember(rec.ID)
		app.LogInfo("external", "bind sql file", "console", rec.ID, "conn", connID, "path", path)
		last = rec.ID
	}
	a.extPickOpen = false
	a.extPickPaths = nil
	if last != "" {
		if !a.activateConsole(last) {
			a.extMsg = "打开控制台失败"
		}
	}
}

// externalBanner 在编辑器上方显示外部文件冲突/缺失。
func (a *root) externalBanner(c *ui.Context) {
	rec, st, ok := a.activeExternal()
	if !ok || (!st.conflict && !st.missing) {
		return
	}
	_ = rec
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		msg := "文件已在外部修改，自动保存已暂停"
		col := t.Danger
		if st.missing {
			msg = "文件已被删除，自动保存已暂停"
		} else if !st.conflict {
			msg = ""
		}
		if msg != "" {
			ui.Text(c, msg).FontSize(12).TextColor(col).SingleLine()
		}
		ui.Spacer(c)
		if st.missing {
			if ui.Button(c, "重新创建").Clicked() {
				a.recreateExternal()
			}
		} else {
			if ui.Button(c, "重新加载").Clicked() {
				a.reloadExternal()
			}
			if ui.Button(c, "保留本地").Clicked() {
				a.keepLocalExternal()
			}
		}
		if ui.Button(c, "另存为…").Clicked() {
			a.pickExternalSaveAs()
		}
	})
}

// externalResolveModal 在执行/保存被外部改动挡住时弹出。
func (a *root) externalResolveModal(c *ui.Context) {
	rec, st, ok := a.activeExternal()
	if !ok {
		return
	}
	ui.Modal(c, &a.extResolveOpen, func() {
		ui.Text(c, "外部文件已变化").Bold()
		ui.Text(c, rec.FilePath).Font("monospace").FontSize(11).
			TextColor(c.Theme().TextMuted).SingleLine().Tooltip(rec.FilePath)
		ui.Text(c, "继续操作前请选择保留哪一侧；取消则保持当前缓冲不动。").
			FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.extResolveOpen = false
			}
			if st.missing {
				if ui.PrimaryButton(c, "另存为…").Clicked() {
					a.extResolveOpen = false
					a.pickExternalSaveAs()
				}
				if ui.Button(c, "重新创建").Clicked() {
					a.extResolveOpen = false
					a.recreateExternal()
				}
				return
			}
			if ui.Button(c, "重新加载").Clicked() {
				a.extResolveOpen = false
				a.reloadExternal()
			}
			if ui.Button(c, "保留本地").Clicked() {
				a.extResolveOpen = false
				a.keepLocalExternal()
			}
			if ui.PrimaryButton(c, "另存为…").Clicked() {
				a.extResolveOpen = false
				a.pickExternalSaveAs()
			}
		})
	}).Width(620).Height(240)
}

// externalPickerModal 让拖入的新文件选择数据源。
func (a *root) externalPickerModal(c *ui.Context) {
	ui.Modal(c, &a.extPickOpen, func() {
		ui.Text(c, "选择数据源").Bold()
		names := make([]string, 0, len(a.extPickPaths))
		for _, p := range a.extPickPaths {
			names = append(names, filepath.Base(p))
		}
		ui.Text(c, "绑定到数据源："+strings.Join(names, "、")).FontSize(12).
			TextColor(c.Theme().TextMuted).SingleLine().Tooltip(strings.Join(a.extPickPaths, "\n"))
		ui.Scroll(c).Height(220).Children(func() {
			if a.store == nil {
				return
			}
			if len(a.store.List()) == 0 {
				ui.Text(c, "还没有数据源，请先新建连接").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for _, p := range a.store.List() {
				if sideNavRow(c, kindLabel(p.Kind)+" · "+p.Name, connKindIcon(p.Kind), connKindColor(p.Kind), false).Clicked() {
					a.bindExternal(p.ID)
				}
			}
		})
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.extPickOpen = false
				a.extPickPaths = nil
			}
		})
	}).Width(520).Height(340)
}

// pickExternalSaveAs 弹保存框，把当前缓冲写到新文件并改绑控制台。
func (a *root) pickExternalSaveAs() {
	rec, _, ok := a.activeExternal()
	if !ok || a.win == nil {
		return
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "另存 SQL 文件",
		DefaultPath: filepath.Base(rec.FilePath),
		Filters:     []mygo.FileFilter{{Name: "SQL", Extensions: []string{"sql"}}},
	})
	if err != nil || path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".sql"
	}
	if err := a.rebindExternal(rec.ID, path); err != nil {
		a.extMsg = "另存失败：" + err.Error()
		return
	}
	app.LogInfo("external", "save as", "console", rec.ID, "path", path)
	a.extMsg = "已另存为 " + path
}

// rebindExternal 把缓冲写到 newPath 并把控制台改绑过去（测试直接调用）。
func (a *root) rebindExternal(id, newPath string) error {
	norm, err := app.NormalizeExternalPath(newPath)
	if err != nil {
		return err
	}
	if other, ok := a.store.GetConsoleByFilePath(norm); ok && other.ID != id {
		return fmt.Errorf("该文件已绑定到控制台 %s", other.Name)
	}
	text := a.localText(id)
	if err := app.WriteExternalSQL(norm, text); err != nil {
		return err
	}
	if err := a.store.RebindConsoleFile(id, norm, filepath.Base(norm)); err != nil {
		return err
	}
	m := a.consoleMgr()
	if m != nil {
		m.SetExternalText(id, text)
		m.SetSavePaused(id, false)
	}
	st := a.externalState(id)
	st.path = norm
	st.lastDisk = text
	st.known, st.conflict, st.missing = true, false, false
	fs := app.StatExternalSQL(norm)
	st.mod, st.size = fs.ModTime, fs.Size
	return nil
}

// reloadExternal 用磁盘内容替换本地缓冲。
func (a *root) reloadExternal() {
	rec, st, ok := a.activeExternal()
	if !ok {
		return
	}
	text, err := app.ReadExternalSQL(rec.FilePath)
	if err != nil {
		st.missing = true
		a.extMsg = "读取失败：" + err.Error()
		return
	}
	m := a.consoleMgr()
	if m != nil {
		m.SetExternalText(rec.ID, text)
		m.SetSavePaused(rec.ID, false)
	}
	st.lastDisk = text
	st.known, st.conflict, st.missing = true, false, false
	fs := app.StatExternalSQL(rec.FilePath)
	st.mod, st.size = fs.ModTime, fs.Size
	a.code.SetActive(rec.ID, text)
	a.lastVer[rec.ID] = a.code.Version()
}

// keepLocalExternal 明确选择本地版本：恢复写盘并立即覆盖磁盘。
func (a *root) keepLocalExternal() {
	rec, st, ok := a.activeExternal()
	if !ok {
		return
	}
	m := a.consoleMgr()
	if m == nil {
		return
	}
	m.SetSavePaused(rec.ID, false)
	if err := m.FlushForce(rec.ID); err != nil {
		m.SetSavePaused(rec.ID, true)
		a.extMsg = "保存失败：" + err.Error()
		return
	}
	text := a.localText(rec.ID)
	st.lastDisk = text
	st.known, st.conflict, st.missing = true, false, false
	fs := app.StatExternalSQL(rec.FilePath)
	st.mod, st.size = fs.ModTime, fs.Size
}

// recreateExternal 文件丢失时按原路径重建（父目录必须存在）。
func (a *root) recreateExternal() {
	rec, st, ok := a.activeExternal()
	if !ok {
		return
	}
	text := a.localText(rec.ID)
	if err := app.WriteExternalSQL(rec.FilePath, text); err != nil {
		a.extMsg = "重新创建失败：" + err.Error()
		return
	}
	m := a.consoleMgr()
	if m != nil {
		m.SetExternalText(rec.ID, text)
		m.SetSavePaused(rec.ID, false)
	}
	st.lastDisk = text
	st.known, st.conflict, st.missing = true, false, false
	fs := app.StatExternalSQL(rec.FilePath)
	st.mod, st.size = fs.ModTime, fs.Size
}

// externalHasLocalChanges 报告外部控制台是否有不能丢的本地内容（冲突，或缺失但脏）。
func (a *root) externalHasLocalChanges(id string) bool {
	if _, ok := a.externalRecord(id); !ok {
		return false
	}
	st := a.externalState(id)
	if st.conflict {
		return true
	}
	m := a.consoleMgr()
	return st.missing && m != nil && m.Dirty(id)
}

// firstBlockedExternal 返回第一个需要用户先处理的外部控制台（窗口关闭保护用）。
func (a *root) firstBlockedExternal() string {
	if a.mgr == nil {
		return ""
	}
	for _, rec := range a.mgr.Open() {
		if rec.External && a.externalHasLocalChanges(rec.ID) {
			return rec.ID
		}
	}
	return ""
}

// externalBlocked 报告活动外部控制台是否因冲突/缺失不能执行或保存。
func (a *root) externalBlocked() bool {
	_, st, ok := a.activeExternal()
	if !ok {
		return false
	}
	if !st.conflict && !st.missing {
		return false
	}
	a.extResolveOpen = true
	return true
}

// externalFlushFailed 处理外部控制台写盘失败：刷新状态并弹出冲突处理。
func (a *root) externalFlushFailed(err error) bool {
	rec, ok := a.externalRecord(a.activeConsole)
	if !ok {
		return false
	}
	a.ensureExternal(rec.ID)
	a.extMsg = "保存失败：" + err.Error()
	a.externalBlocked()
	return true
}

// externalAfterSave 写盘后刷新基线（自动保存的定时器不经过这里，轮询会自行对齐）。
func (a *root) externalAfterSave(id string) {
	if rec, ok := a.externalRecord(id); ok {
		st := a.externalState(id)
		fs := app.StatExternalSQL(rec.FilePath)
		if fs.Exists {
			st.lastDisk = a.localText(id)
			st.mod, st.size = fs.ModTime, fs.Size
			st.known, st.missing, st.conflict = true, false, false
		}
	}
}

// RemoveExternalState 丢弃某控制台的外部状态（删除控制台时用）。
func (a *root) removeExternalState(id string) {
	delete(a.extFiles, id)
}
