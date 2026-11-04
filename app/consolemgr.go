package app

import (
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

	mu         sync.Mutex
	cache      map[string]string // consoleID → 最近正文
	lastSaved  map[string]string // consoleID → 最近成功写盘的内容
	dirty      map[string]bool   // consoleID → 有待写改动
	saveTimers map[string]*time.Timer
	caretDraft map[string][2]int // consoleID → 尚未落库的光标/选区（rune 偏移）
	caretTimer map[string]*time.Timer
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
	}
}

// Store 暴露底层存储（工作区/控制台元数据的其余操作）。
func (m *ConsoleManager) Store() *ConnStore { return m.store }

// Text 返回控制台正文；首次访问读盘（文件缺失 = 空串），未知 id 返回 false。
func (m *ConsoleManager) Text(id string) (string, bool) {
	if _, ok := m.store.GetConsole(id); !ok {
		return "", false
	}
	m.mu.Lock()
	if text, ok := m.cache[id]; ok {
		m.mu.Unlock()
		return text, true
	}
	m.mu.Unlock()

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

// Dirty 报告是否有未写盘的改动。
func (m *ConsoleManager) Dirty(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dirty[id]
}

// Flush 立即把该控制台的正文写盘；成功后清脏并记录 UpdatedAt。失败保留脏标记。
func (m *ConsoleManager) Flush(id string) error {
	m.mu.Lock()
	if !m.dirty[id] {
		m.mu.Unlock()
		return nil
	}
	text := m.cache[id]
	m.stopSaveTimerLocked(id)
	m.mu.Unlock()

	if err := m.sink.Write(id, text); err != nil {
		return err
	}

	m.mu.Lock()
	m.lastSaved[id] = text
	if m.cache[id] == text {
		delete(m.dirty, id)
	}
	m.mu.Unlock()
	m.store.TouchConsole(id, time.Now().UnixMilli())
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
}
