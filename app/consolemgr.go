package app

import (
	"errors"
	"sync"
	"time"
)

// ConsoleManager 编排控制台的正文、脏标记、自动保存与光标记忆。
//
// 活动控制台的正文以 UI 的 TextBuffer 为权威，管理器缓存其余控制台的正文。
// 所有对外方法可被 UI 主线程与防抖定时器并发调用。
type ConsoleManager struct {
	// SaveDelay 是编辑到写盘的防抖间隔；CaretDelay 是光标落库的防抖间隔。
	SaveDelay  time.Duration
	CaretDelay time.Duration

	store *ConnStore
	sink  TextSink

	mu                sync.Mutex
	activeWorkspaceID string            // 内存里的活动工作区（持久化副本在 store）
	activeConsoleID   string            // 当前展示的控制台
	cache             map[string]string // consoleID → 最近正文
	lastSaved         map[string]string // consoleID → 最近成功写盘的内容
	dirty             map[string]bool   // consoleID → 有待写改动
	saveTimers        map[string]*time.Timer
	caretDraft        map[string][2]int // consoleID → 尚未落库的光标/选区（rune 偏移）
	caretTimer        map[string]*time.Timer
	src               map[string]func() string // consoleID → 惰性正文来源（活动控制台）
	ver               map[string]uint64        // consoleID → 已见过的正文版本
	paused            map[string]bool          // consoleID → 暂停写盘（外部文件冲突/缺失）
}

// NewConsoleManager 创建管理器；sink 的生产实现是 ConsoleFiles。
func NewConsoleManager(store *ConnStore, sink TextSink) *ConsoleManager {
	return &ConsoleManager{
		SaveDelay:  700 * time.Millisecond,
		CaretDelay: 1500 * time.Millisecond,
		store:      store,
		sink:       sink,
		cache:      map[string]string{},
		lastSaved:  map[string]string{},
		dirty:      map[string]bool{},
		saveTimers: map[string]*time.Timer{},
		caretDraft: map[string][2]int{},
		caretTimer: map[string]*time.Timer{},
		src:        map[string]func() string{},
		ver:        map[string]uint64{},
		paused:     map[string]bool{},
	}
}

// Store 暴露底层存储（工作区/控制台元数据的其余操作）。
func (m *ConsoleManager) Store() *ConnStore { return m.store }

// Text 返回控制台已保存的正文（内存缓冲优先，其次磁盘；文件缺失 = 空串）。
// 它绝不读活动控制台的惰性编辑器来源：编辑器内容只在 Flush 时可见，
// 否则「切到别的控制台」会读到当前编辑器文本而不是目标控制台自己的正文。
func (m *ConsoleManager) Text(id string) (string, bool) {
	if _, ok := m.store.GetConsole(id); !ok {
		return "", false
	}
	m.mu.Lock()
	text, ok := m.cache[id]
	m.mu.Unlock()
	if ok {
		return text, true
	}

	text, err := m.sink.Read(id)
	if err != nil {
		text = ""
	}
	m.mu.Lock()
	if cached, ok := m.cache[id]; ok {
		m.mu.Unlock()
		return cached, true
	}
	m.cache[id] = text
	if _, ok := m.lastSaved[id]; !ok {
		m.lastSaved[id] = text
	}
	m.mu.Unlock()
	return text, true
}

// SetText 记录编辑内容：内容不变时不置脏、不起防抖；变化则重置防抖写盘。
func (m *ConsoleManager) SetText(id, text string) {
	m.mu.Lock()
	if cached, ok := m.cache[id]; ok && cached == text {
		m.mu.Unlock()
		return
	}
	if _, ok := m.cache[id]; !ok {
		// 首次编辑先认一下磁盘内容，避免把「没读到过」当成脏。
		if _, known := m.lastSaved[id]; !known {
			if disk, err := m.sink.Read(id); err == nil {
				m.lastSaved[id] = disk
			}
		}
	}
	m.cache[id] = text
	saved := m.lastSaved[id]
	m.dirty[id] = text != saved
	m.stopSaveTimerLocked(id)
	if m.dirty[id] {
		delay := m.SaveDelay
		m.saveTimers[id] = time.AfterFunc(delay, func() { _ = m.Flush(id) })
	}
	m.mu.Unlock()
}

// SetTextSource 注册活动控制台的惰性正文来源：逐帧只比较版本号（常数级），
// 真正取文本（O(n) 拷贝）只在落盘时发生。
func (m *ConsoleManager) SetTextSource(id string, src func() string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if src == nil {
		delete(m.src, id)
		return
	}
	m.src[id] = src
}

// DetachTextSource 解除某控制台的编辑器来源并丢弃其内存正文/版本：
// 之后 Text 重新读盘。切换控制台时对「旧控制台」和「目标控制台」各调一次，
// 保证两边都以磁盘（刚由 Flush 写下的内容）为准。
func (m *ConsoleManager) DetachTextSource(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.src, id)
	delete(m.ver, id)
	if !m.dirty[id] {
		// 已落盘：下次 Text 重新读盘（磁盘权威）；仍有未落盘改动时保留缓存，避免丢内容。
		delete(m.cache, id)
	}
}

// TouchVersion 报告编辑器正文版本变化：只置脏并重置防抖，不拷贝正文。
func (m *ConsoleManager) TouchVersion(id string, ver uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.src[id] == nil {
		return
	}
	if m.ver[id] == ver {
		return
	}
	m.ver[id] = ver
	m.dirty[id] = true
	m.stopSaveTimerLocked(id)
	delay := m.SaveDelay
	m.saveTimers[id] = time.AfterFunc(delay, func() { _ = m.Flush(id) })
}

// ErrExternalChanged 表示外部文件在内存缓冲之外被修改，写入会覆盖别人的改动。
var ErrExternalChanged = errors.New("外部文件已被修改")

// externalSink 是 ConsoleFiles 的可选能力：区分外部文件控制台。
type externalSink interface{ External(id string) bool }

// SetSavePaused 暂停/恢复自动写盘：外部文件被改或丢失时暂停，避免覆盖/重建。
// 暂停期间 Flush 保持脏标记并返回 nil（数据留在内存与编辑器里）。
func (m *ConsoleManager) SetSavePaused(id string, paused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if paused {
		m.paused[id] = true
		m.stopSaveTimerLocked(id)
		return
	}
	delete(m.paused, id)
}

// SetExternalText 用磁盘内容重置内存缓冲（外部文件干净时自动重载）。
func (m *ConsoleManager) SetExternalText(id, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopSaveTimerLocked(id)
	m.cache[id] = text
	m.lastSaved[id] = text
	delete(m.dirty, id)
}

// isExternal 报告控制台是否绑定外部文件（需要写前一致性校验）。
func (m *ConsoleManager) isExternal(id string) bool {
	if s, ok := m.sink.(externalSink); ok {
		return s.External(id)
	}
	return false
}

// Dirty 报告是否有未写盘的改动。
func (m *ConsoleManager) Dirty(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dirty[id]
}

// Flush 立即把该控制台的正文写盘；成功后清脏并记录 UpdatedAt。失败保留脏标记。
func (m *ConsoleManager) Flush(id string) error { return m.flush(id, false) }

// FlushForce 跳过外部文件一致性校验（用户明确选择「保留本地」时用）。
func (m *ConsoleManager) FlushForce(id string) error { return m.flush(id, true) }

func (m *ConsoleManager) flush(id string, force bool) error {
	m.mu.Lock()
	if m.paused[id] {
		m.stopSaveTimerLocked(id)
		m.mu.Unlock()
		return nil
	}
	if !m.dirty[id] {
		m.mu.Unlock()
		return nil
	}
	src, text := m.src[id], m.cache[id]
	m.stopSaveTimerLocked(id)
	m.mu.Unlock()

	if src != nil {
		text = src()
	}

	m.mu.Lock()
	saved, known := m.lastSaved[id]
	if known && saved == text {
		// 内容与上次落盘一致（例如撤销回原文）：不必重写。
		delete(m.dirty, id)
		m.mu.Unlock()
		return nil
	}
	if !force && m.isExternal(id) && saved != text {
		// 外部文件：写前确认磁盘仍是上次见到的那版，避免覆盖外部编辑。
		disk, err := m.sink.Read(id)
		if err != nil || disk != saved {
			m.mu.Unlock()
			return ErrExternalChanged
		}
	}
	m.mu.Unlock()

	if err := m.sink.Write(id, text); err != nil {
		return err
	}
	// 先把 UpdatedAt 落库，再清脏：Dirty=false 必须意味着正文和元数据都已写完，
	// 否则测试/退出路径会在后台写盘尚未结束时继续清理目录。
	m.store.TouchConsole(id, time.Now().UnixMilli())

	m.mu.Lock()
	m.lastSaved[id] = text
	if src != nil || m.cache[id] == text {
		delete(m.dirty, id)
	}
	m.mu.Unlock()
	return nil
}

// FlushAll 写盘所有脏控制台（退出/Ctrl+S 前用）。
func (m *ConsoleManager) FlushAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.dirty))
	for id := range m.dirty {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Flush(id)
	}
}

// Caret 返回控制台上次的光标/选区（rune 偏移）：内存草稿优先，其次元数据。
func (m *ConsoleManager) Caret(id string) (int, int) {
	m.mu.Lock()
	if pair, ok := m.caretDraft[id]; ok {
		m.mu.Unlock()
		return pair[0], pair[1]
	}
	m.mu.Unlock()
	if rec, ok := m.store.GetConsole(id); ok {
		return rec.CaretStart, rec.CaretEnd
	}
	return 0, 0
}

// SetCaret 记录光标/选区：内存即时生效，防抖落库。
func (m *ConsoleManager) SetCaret(id string, start, end int) {
	m.mu.Lock()
	if cur, ok := m.caretDraft[id]; ok && cur == [2]int{start, end} {
		m.mu.Unlock()
		return
	}
	if rec, ok := m.store.GetConsole(id); ok && rec.CaretStart == start && rec.CaretEnd == end {
		if _, drafted := m.caretDraft[id]; !drafted {
			m.mu.Unlock()
			return
		}
	}
	m.caretDraft[id] = [2]int{start, end}
	if t, ok := m.caretTimer[id]; ok {
		t.Stop()
	}
	delay := m.CaretDelay
	m.caretTimer[id] = time.AfterFunc(delay, func() { m.flushCaret(id) })
	m.mu.Unlock()
}

// FlushCarets 立即落库所有光标草稿。
func (m *ConsoleManager) FlushCarets() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.caretDraft))
	for id := range m.caretDraft {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.flushCaret(id)
	}
}

func (m *ConsoleManager) flushCaret(id string) {
	m.mu.Lock()
	pair, ok := m.caretDraft[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.caretDraft, id)
	if t, ok := m.caretTimer[id]; ok {
		t.Stop()
		delete(m.caretTimer, id)
	}
	m.mu.Unlock()
	m.store.SetConsoleCaret(id, pair[0], pair[1])
}

func (m *ConsoleManager) stopSaveTimerLocked(id string) {
	if t, ok := m.saveTimers[id]; ok {
		t.Stop()
		delete(m.saveTimers, id)
	}
}

// forget 丢弃某控制台的缓存/脏标记/草稿（关连接、删控制台时用）。
func (m *ConsoleManager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopSaveTimerLocked(id)
	if t, ok := m.caretTimer[id]; ok {
		t.Stop()
		delete(m.caretTimer, id)
	}
	delete(m.cache, id)
	delete(m.lastSaved, id)
	delete(m.dirty, id)
	delete(m.caretDraft, id)
	delete(m.src, id)
	delete(m.ver, id)
}

// ---------- 工作区与激活 ----------

// Workspaces 返回全部工作区（按 SortOrder）。
func (m *ConsoleManager) Workspaces() []WorkspaceRecord { return m.store.ListWorkspaces() }

// ActiveWorkspace 返回当前工作区；内存指针无效时回落到第一个（不写盘）。
func (m *ConsoleManager) ActiveWorkspace() (WorkspaceRecord, bool) {
	ws, ok := m.resolveActiveWorkspace()
	if !ok {
		return WorkspaceRecord{}, false
	}
	return ws, true
}

func (m *ConsoleManager) resolveActiveWorkspace() (WorkspaceRecord, bool) {
	list := m.store.ListWorkspaces()
	if len(list) == 0 {
		m.mu.Lock()
		m.activeWorkspaceID = ""
		m.mu.Unlock()
		return WorkspaceRecord{}, false
	}
	m.mu.Lock()
	id := m.activeWorkspaceID
	m.mu.Unlock()
	for _, ws := range list {
		if ws.ID == id {
			return ws, true
		}
	}
	m.mu.Lock()
	m.activeWorkspaceID = list[0].ID
	m.mu.Unlock()
	return list[0], true
}

// EnsureWorkspace 保证存在一个激活工作区：优先当前的，否则第一个，都没有就建「默认」。
func (m *ConsoleManager) EnsureWorkspace() (WorkspaceRecord, error) {
	if ws, ok := m.resolveActiveWorkspace(); ok {
		return ws, nil
	}
	ws, err := m.store.CreateWorkspace("")
	if err != nil {
		return WorkspaceRecord{}, err
	}
	m.store.SetActiveWorkspace(ws.ID)
	m.mu.Lock()
	m.activeWorkspaceID = ws.ID
	m.mu.Unlock()
	return ws, nil
}

// CreateWorkspace 新建工作区并激活它（无成员，激活控制台清空）。
func (m *ConsoleManager) CreateWorkspace(name string) (WorkspaceRecord, error) {
	m.flushActiveState() // 切走前把当前控制台的正文/光标落盘
	ws, err := m.store.CreateWorkspace(name)
	if err != nil {
		return WorkspaceRecord{}, err
	}
	m.store.SetActiveWorkspace(ws.ID)
	m.mu.Lock()
	m.activeWorkspaceID = ws.ID
	m.activeConsoleID = ""
	m.mu.Unlock()
	return ws, nil
}

// SetActiveWorkspace 切换工作区，并按它的 LastActiveConsoleID（无效则最后成员）恢复激活控制台。
func (m *ConsoleManager) SetActiveWorkspace(id string) (WorkspaceRecord, bool) {
	ws, ok := m.store.GetWorkspace(id)
	if !ok {
		return WorkspaceRecord{}, false
	}
	m.flushActiveState()
	m.store.SetActiveWorkspace(ws.ID)
	m.mu.Lock()
	m.activeWorkspaceID = ws.ID
	m.activeConsoleID = ""
	m.mu.Unlock()
	m.restoreActiveIn(ws)
	return ws, true
}

// RenameWorkspace 改名。
func (m *ConsoleManager) RenameWorkspace(id, name string) error {
	return m.store.RenameWorkspace(id, name)
}

// DeleteWorkspace 删除工作区；删的是当前工作区时切到下一个（无则进入无工作区态）。
func (m *ConsoleManager) DeleteWorkspace(id string) error {
	wasActive := false
	if ws, ok := m.ActiveWorkspace(); ok && ws.ID == id {
		wasActive = true
	}
	m.flushActiveState()
	if err := m.store.DeleteWorkspace(id); err != nil {
		return err
	}
	if !wasActive {
		return nil
	}
	m.mu.Lock()
	m.activeWorkspaceID = ""
	m.activeConsoleID = ""
	m.mu.Unlock()
	if ws, ok := m.resolveActiveWorkspace(); ok {
		m.store.SetActiveWorkspace(ws.ID)
		m.restoreActiveIn(ws)
	}
	return nil
}

// ---------- 控制台集合与开关 ----------

// ConsolesOf 返回某数据源的全部控制台（不看工作区成员关系）。
func (m *ConsoleManager) ConsolesOf(connID string) []ConsoleRecord {
	return m.store.ListConsoles(connID)
}

// Open 返回当前工作区的成员（按成员顺序），跳过指向已删除控制台的悬空 id。
func (m *ConsoleManager) Open() []ConsoleRecord {
	ws, ok := m.ActiveWorkspace()
	if !ok {
		return nil
	}
	out := make([]ConsoleRecord, 0, len(ws.Members))
	for _, id := range ws.Members {
		if rec, ok := m.store.GetConsole(id); ok {
			out = append(out, rec)
		}
	}
	return out
}

// IsOpen 报告控制台是否在当前工作区。
func (m *ConsoleManager) IsOpen(id string) bool {
	ws, ok := m.ActiveWorkspace()
	if !ok {
		return false
	}
	for _, member := range ws.Members {
		if member == id {
			return true
		}
	}
	return false
}

// Active 返回当前激活的控制台；指针无效或已不是当前工作区成员时清空。
func (m *ConsoleManager) Active() (ConsoleRecord, bool) {
	m.mu.Lock()
	id := m.activeConsoleID
	m.mu.Unlock()
	if id == "" {
		// 冷启动：按激活工作区的 LastActiveConsoleID（无效 → 最后成员）惰性恢复。
		if ws, ok := m.ActiveWorkspace(); ok && len(ws.Members) > 0 {
			m.restoreActiveIn(ws)
			m.mu.Lock()
			id = m.activeConsoleID
			m.mu.Unlock()
		}
	}
	if id == "" || !m.IsOpen(id) {
		if id != "" {
			m.mu.Lock()
			if m.activeConsoleID == id {
				m.activeConsoleID = ""
			}
			m.mu.Unlock()
		}
		return ConsoleRecord{}, false
	}
	rec, ok := m.store.GetConsole(id)
	if !ok {
		m.mu.Lock()
		m.activeConsoleID = ""
		m.mu.Unlock()
		return ConsoleRecord{}, false
	}
	return rec, true
}

// Create 在数据源下新建控制台：建行 + 空文件 + 加入当前工作区 + 激活。
func (m *ConsoleManager) Create(connID, name string) (ConsoleRecord, error) {
	ws, err := m.EnsureWorkspace()
	if err != nil {
		return ConsoleRecord{}, err
	}
	rec, err := m.store.CreateConsole(connID, name)
	if err != nil {
		return ConsoleRecord{}, err
	}
	if err := m.sink.Write(rec.ID, ""); err != nil {
		_ = m.store.DeleteConsole(rec.ID)
		return ConsoleRecord{}, err
	}
	if err := m.store.AddWorkspaceMember(ws.ID, rec.ID); err != nil {
		_ = m.sink.Delete(rec.ID)
		_ = m.store.DeleteConsole(rec.ID)
		return ConsoleRecord{}, err
	}
	m.activate(rec.ID)
	return rec, nil
}

// OpenConsole 把控制台加入当前工作区（末尾）并激活；正文重新读盘。
func (m *ConsoleManager) OpenConsole(id string) (ConsoleRecord, bool) {
	rec, ok := m.store.GetConsole(id)
	if !ok {
		return ConsoleRecord{}, false
	}
	ws, err := m.EnsureWorkspace()
	if err != nil {
		return ConsoleRecord{}, false
	}
	if !m.IsOpen(id) {
		if err := m.store.AddWorkspaceMember(ws.ID, id); err != nil {
			return ConsoleRecord{}, false
		}
		m.forget(id) // 真正重开：正文以磁盘为准
	}
	m.activate(id)
	return rec, true
}

// CloseConsole 把控制台移出当前工作区（正文/元数据/结果保留）；关的是激活控制台时切换。
func (m *ConsoleManager) CloseConsole(id string) error {
	ws, ok := m.ActiveWorkspace()
	if !ok {
		return nil
	}
	if err := m.Flush(id); err != nil {
		return err
	}
	m.flushCaret(id)
	if err := m.store.RemoveWorkspaceMember(ws.ID, id); err != nil {
		return err
	}
	m.mu.Lock()
	wasActive := m.activeConsoleID == id
	m.mu.Unlock()
	if wasActive {
		m.mu.Lock()
		m.activeConsoleID = ""
		m.mu.Unlock()
		if ws, ok := m.store.GetWorkspace(ws.ID); ok {
			m.restoreActiveIn(ws)
		}
	}
	return nil
}

// DeleteConsole 删除控制台：元数据 + .sql 文件 + 所有工作区成员。
func (m *ConsoleManager) DeleteConsole(id string) error {
	if _, ok := m.store.GetConsole(id); !ok {
		return nil
	}
	_ = m.Flush(id)
	m.forget(id)
	wasActive := false
	m.mu.Lock()
	wasActive = m.activeConsoleID == id
	m.mu.Unlock()
	ws, hadWS := m.ActiveWorkspace()
	if err := m.store.DeleteConsole(id); err != nil {
		return err
	}
	_ = m.sink.Delete(id)
	if wasActive {
		m.mu.Lock()
		m.activeConsoleID = ""
		m.mu.Unlock()
		if hadWS {
			if ws, ok := m.store.GetWorkspace(ws.ID); ok {
				m.restoreActiveIn(ws)
			}
		}
	}
	return nil
}

// Rename 重命名控制台（文件名不变）。
func (m *ConsoleManager) Rename(id, name string) error { return m.store.RenameConsole(id, name) }

// OnConnectionDeleted 清理被删连接的控制台：缓存/定时器 + 正文文件 + 激活态。
// recs 必须是删除连接之前收集的控制台列表。
func (m *ConsoleManager) OnConnectionDeleted(connID string, recs []ConsoleRecord) {
	activeGone := false
	m.mu.Lock()
	active := m.activeConsoleID
	for _, rec := range recs {
		if rec.ID == active {
			activeGone = true
		}
	}
	m.mu.Unlock()
	for _, rec := range recs {
		m.forget(rec.ID)
		_ = m.sink.Delete(rec.ID)
	}
	if activeGone {
		m.mu.Lock()
		m.activeConsoleID = ""
		m.mu.Unlock()
	}
	if ws, ok := m.ActiveWorkspace(); ok {
		if fresh, ok := m.store.GetWorkspace(ws.ID); ok {
			m.mu.Lock()
			empty := m.activeConsoleID == ""
			m.mu.Unlock()
			if empty {
				m.restoreActiveIn(fresh)
			}
		}
	}
	_ = connID
}

// activate 设定激活控制台并记忆到工作区。
func (m *ConsoleManager) activate(id string) {
	m.mu.Lock()
	m.activeConsoleID = id
	m.mu.Unlock()
	if ws, ok := m.ActiveWorkspace(); ok {
		_ = m.store.SetWorkspaceLastActive(ws.ID, id)
	}
}

// restoreActiveIn 按工作区的 LastActiveConsoleID（无效 → 最后成员）恢复激活；无成员则清空。
func (m *ConsoleManager) restoreActiveIn(ws WorkspaceRecord) {
	m.mu.Lock()
	m.activeConsoleID = ""
	m.mu.Unlock()
	if len(ws.Members) == 0 {
		return
	}
	pick := ""
	if rec, ok := m.store.GetConsole(ws.LastActiveConsoleID); ok && m.IsOpen(rec.ID) {
		pick = rec.ID
	} else {
		pick = ws.Members[len(ws.Members)-1]
	}
	if _, ok := m.store.GetConsole(pick); !ok {
		return
	}
	m.activate(pick)
}

// flushActiveState 落盘当前激活控制台的正文与光标（切工作区/删工作区前）。
func (m *ConsoleManager) flushActiveState() {
	m.mu.Lock()
	id := m.activeConsoleID
	m.mu.Unlock()
	if id == "" {
		return
	}
	_ = m.Flush(id)
	m.flushCaret(id)
}
