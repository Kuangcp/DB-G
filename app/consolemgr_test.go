package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu     sync.Mutex
	files  map[string]string
	fail   error
	writes int
}

func newFakeSink() *fakeSink { return &fakeSink{files: map[string]string{}} }

func (f *fakeSink) Read(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[id], nil
}

func (f *fakeSink) Write(id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.files[id] = text
	f.writes++
	return nil
}

func (f *fakeSink) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, id)
	return nil
}

func (f *fakeSink) Exists(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[id]
	return ok
}

// text 是并发安全的读值（防抖定时器在别的 goroutine 写）。
func (f *fakeSink) text(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[id]
}

func newMgr(t *testing.T, sink TextSink) (*ConnStore, *ConnProfile, *ConsoleRecord, *ConsoleManager) {
	t.Helper()
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, err := s.CreateConsole(p.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	m := NewConsoleManager(s, sink)
	m.SaveDelay, m.CaretDelay = 10*time.Millisecond, 10*time.Millisecond
	return s, &p, &rec, m
}

func TestSetTextDebouncesWrite(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	if text, ok := m.Text(rec.ID); !ok || text != "" {
		t.Fatalf("text: %q %v", text, ok)
	}
	m.SetText(rec.ID, "SELECT 1")
	m.SetText(rec.ID, "SELECT 1;")
	if !m.Dirty(rec.ID) {
		t.Fatal("must be dirty right after edit")
	}
	waitFor(t, 2*time.Second, func() bool { return !m.Dirty(rec.ID) })
	if got := sink.text(rec.ID); got != "SELECT 1;" {
		t.Fatalf("written: %q", got)
	}
}

func TestSetTextUnchangedIsNoop(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "")
	if m.Dirty(rec.ID) {
		t.Fatal("same text must not mark dirty")
	}
}

func TestFlushWritesImmediately(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "SELECT 42")
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 42" || m.Dirty(rec.ID) {
		t.Fatalf("flush: %q dirty=%v", got, m.Dirty(rec.ID))
	}
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal("second flush must be a no-op:", err)
	}
	if sink.writes != 1 {
		t.Fatalf("writes: %d", sink.writes)
	}
}

func TestFlushFailureKeepsDirty(t *testing.T) {
	sink := newFakeSink()
	sink.fail = errors.New("disk full")
	_, _, rec, m := newMgr(t, sink)
	m.SetText(rec.ID, "SELECT 7")
	if err := m.Flush(rec.ID); err == nil {
		t.Fatal("must report write failure")
	}
	if !m.Dirty(rec.ID) {
		t.Fatal("failed write must keep content dirty")
	}
	sink.fail = nil
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 7" || m.Dirty(rec.ID) {
		t.Fatalf("retry: %q dirty=%v", got, m.Dirty(rec.ID))
	}
}

func TestCaretPersistsAcrossManagers(t *testing.T) {
	s, _, rec, m := newMgr(t, newFakeSink())
	m.SetCaret(rec.ID, 4, 9)
	if start, end := m.Caret(rec.ID); start != 4 || end != 9 {
		t.Fatalf("immediate caret: %d %d", start, end)
	}
	waitFor(t, 2*time.Second, func() bool {
		got, _ := s.GetConsole(rec.ID)
		return got.CaretStart == 4 && got.CaretEnd == 9
	})
	reloaded := NewConsoleManager(LoadStore(s.path), newFakeSink())
	if start, end := reloaded.Caret(rec.ID); start != 4 || end != 9 {
		t.Fatalf("persisted caret: %d %d", start, end)
	}
}

func TestTextUnknownConsole(t *testing.T) {
	_, _, _, m := newMgr(t, newFakeSink())
	if text, ok := m.Text("nope"); ok || text != "" {
		t.Fatalf("unknown: %q %v", text, ok)
	}
}

func TestCreateAddsMemberAndActivates(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, err := m.Create(p.ID, "第一条")
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsOpen(rec.ID) {
		t.Fatal("created console must be a member of the active workspace")
	}
	if got, ok := m.Active(); !ok || got.ID != rec.ID {
		t.Fatalf("active: %+v %v", got, ok)
	}
	if !sink.Exists(rec.ID) {
		t.Fatal("empty .sql file must be created")
	}
	ws, _ := m.ActiveWorkspace()
	if ws.LastActiveConsoleID != rec.ID {
		t.Fatalf("last active: %+v", ws)
	}
}

func TestCloseKeepsFileAndSwitchesActive(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	a, _ := m.Create(p.ID, "a")
	b, _ := m.Create(p.ID, "b")
	m.SetText(b.ID, "SELECT 2")
	if err := m.CloseConsole(b.ID); err != nil {
		t.Fatal(err)
	}
	if m.IsOpen(b.ID) {
		t.Fatal("closed console must leave the workspace")
	}
	if !sink.Exists(b.ID) {
		t.Fatal("closing must keep the .sql file")
	}
	if got, ok := m.Active(); !ok || got.ID != a.ID {
		t.Fatalf("active after close: %+v %v", got, ok)
	}
	sink.files[b.ID] = "SELECT 99"
	if _, ok := m.OpenConsole(b.ID); !ok {
		t.Fatal("reopen must succeed")
	}
	if text, _ := m.Text(b.ID); text != "SELECT 99" {
		t.Fatalf("reopen must re-read disk: %q", text)
	}
}

func TestOpenConsoleAppendsMember(t *testing.T) {
	_, p, _, m := newMgr(t, newFakeSink())
	a, _ := m.Create(p.ID, "a")
	b, err := m.Store().CreateConsole(p.ID, "b")
	_ = err
	_ = m.CloseConsole(a.ID)
	if _, ok := m.OpenConsole(b.ID); !ok {
		t.Fatal("open unknown-to-workspace console")
	}
	open := m.Open()
	if len(open) != 1 || open[0].ID != b.ID {
		t.Fatalf("open order: %+v", open)
	}
}

func TestOpenSkipsDanglingMemberIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	raw := `{"consoles":[{"id":"c1","connectionID":"p1","name":"a"}],
		"workspaces":[{"id":"w1","name":"默认","members":["c1","deadbeef"]}],
		"activeWorkspaceID":"w1"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewConsoleManager(LoadStore(path), newFakeSink())
	open := m.Open()
	if len(open) != 1 || open[0].ID != "c1" {
		t.Fatalf("dangling member must be skipped: %+v", open)
	}
}

func TestDeleteWorkspaceKeepsConsolesReopenable(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	ws, _ := m.ActiveWorkspace()
	if err := m.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ActiveWorkspace(); ok {
		t.Fatal("no workspace must remain")
	}
	if len(m.Open()) != 0 {
		t.Fatal("open list must be empty without a workspace")
	}
	if !sink.Exists(rec.ID) {
		t.Fatal("file must survive workspace deletion")
	}
	if _, ok := m.Active(); ok {
		t.Fatal("no active console without a workspace")
	}
}

func TestSetActiveWorkspaceRestoresLastActive(t *testing.T) {
	_, p, _, m := newMgr(t, newFakeSink())
	wsA, err := m.EnsureWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := m.Create(p.ID, "a")
	wsB, err := m.CreateWorkspace("B")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.Create(p.ID, "b")
	if got, _ := m.Active(); got.ID != b.ID {
		t.Fatalf("create must activate in the new workspace: %+v", got)
	}
	if _, ok := m.SetActiveWorkspace(wsA.ID); !ok {
		t.Fatal("switch to A")
	}
	if got, _ := m.Active(); got.ID != a.ID {
		t.Fatalf("restore A console: %+v", got)
	}
	if _, ok := m.SetActiveWorkspace(wsB.ID); !ok {
		t.Fatal("switch to B")
	}
	if got, _ := m.Active(); got.ID != b.ID {
		t.Fatalf("restore B console: %+v", got)
	}
}

func TestEnsureWorkspaceCreatesDefaultOnce(t *testing.T) {
	_, _, _, m := newMgr(t, newFakeSink())
	ws1, err := m.EnsureWorkspace()
	if err != nil || ws1.Name != "默认" {
		t.Fatalf("ensure: %+v %v", ws1, err)
	}
	ws2, err := m.EnsureWorkspace()
	if err != nil || ws2.ID != ws1.ID {
		t.Fatalf("ensure must be idempotent: %+v %v", ws2, err)
	}
	if n := len(m.Workspaces()); n != 1 {
		t.Fatalf("workspaces: %d", n)
	}
}

func TestDeleteConsoleRemovesEverywhere(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	m.SetText(rec.ID, "SELECT 1")
	if err := m.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	if sink.Exists(rec.ID) {
		t.Fatal(".sql must be deleted with the console")
	}
	if _, ok := m.Active(); ok {
		t.Fatal("deleted active console must clear activation")
	}
	if len(m.Open()) != 0 {
		t.Fatal("member must be removed")
	}
}

func TestOnConnectionDeletedClearsActive(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "c")
	recs := m.ConsolesOf(p.ID)
	_ = m.Store().Remove(p.ID)
	m.OnConnectionDeleted(p.ID, recs)
	if _, ok := m.Active(); ok {
		t.Fatal("active must clear")
	}
	if sink.Exists(rec.ID) {
		t.Fatal("files of the deleted connection must be removed")
	}
}

func TestCreateWorkspaceFlushesActiveConsole(t *testing.T) {
	sink := newFakeSink()
	_, p, _, m := newMgr(t, sink)
	rec, _ := m.Create(p.ID, "a")
	m.SetText(rec.ID, "SELECT 9")
	if _, err := m.CreateWorkspace("B"); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 9" {
		t.Fatalf("switching workspace must flush the active console: %q", got)
	}
}

// blockingSink 让 Write 阻塞，模拟写盘期间编辑器继续输入。
type blockingSink struct {
	*fakeSink
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (b *blockingSink) Write(id, text string) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.fakeSink.Write(id, text)
}

func TestFlushKeepsDirtyWhenEditRacesWrite(t *testing.T) {
	sink := &blockingSink{fakeSink: newFakeSink(), started: make(chan struct{}), release: make(chan struct{})}
	_, _, rec, m := newMgr(t, sink)
	m.SaveDelay = time.Hour

	var mu sync.Mutex
	text := "v1"
	m.SetTextSource(rec.ID, func() string {
		mu.Lock()
		defer mu.Unlock()
		return text
	})
	m.TouchVersion(rec.ID, 1)

	done := make(chan error, 1)
	go func() { done <- m.Flush(rec.ID) }()
	<-sink.started

	mu.Lock()
	text = "v2"
	mu.Unlock()
	m.TouchVersion(rec.ID, 2)
	if !m.Writing(rec.ID) {
		t.Fatal("写盘期间应报告 Writing")
	}
	close(sink.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.Writing(rec.ID) {
		t.Fatal("写盘结束应清除 Writing")
	}
	if !m.Dirty(rec.ID) {
		t.Fatal("写盘期间的新编辑必须保留脏标记")
	}
	if got, _ := m.LastSaved(rec.ID); got != "v1" {
		t.Fatalf("本次写盘内容: %q", got)
	}
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "v2" {
		t.Fatalf("后续写盘应追上最新编辑: %q", got)
	}
	if m.Dirty(rec.ID) {
		t.Fatal("追上后应清脏")
	}
}

func TestLazyTextSourceFlush(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	text := "SELECT 1"
	m.SetTextSource(rec.ID, func() string { return text })
	m.TouchVersion(rec.ID, 2)
	if !m.Dirty(rec.ID) {
		t.Fatal("version bump must mark dirty")
	}
	text = "SELECT 2"
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "SELECT 2" {
		t.Fatalf("flush must read the source lazily: %q", got)
	}
	if m.Dirty(rec.ID) {
		t.Fatal("flush must clear dirty")
	}
	writes := sink.writes
	m.TouchVersion(rec.ID, 2) // 同一版本：不置脏、不重写
	_ = m.Flush(rec.ID)
	if sink.writes != writes {
		t.Fatalf("unchanged version must not rewrite: writes=%d", sink.writes)
	}
}

func TestLazySourceSkipsIdenticalContent(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	text := "SELECT 1"
	m.SetTextSource(rec.ID, func() string { return text })
	m.TouchVersion(rec.ID, 2)
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	// 版本变了但内容没变（例如撤销回原文）：不应再写一次盘。
	m.TouchVersion(rec.ID, 3)
	writes := sink.writes
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if sink.writes != writes {
		t.Fatal("identical content must not be rewritten")
	}
}

func TestTextIgnoresLiveSource(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	sink.files[rec.ID] = "on disk"
	live := "in editor"
	m.SetTextSource(rec.ID, func() string { return live })
	if got, _ := m.Text(rec.ID); got != "on disk" {
		t.Fatalf("Text must read stored body, not the live editor source: %q", got)
	}
	m.SetText(rec.ID, "explicit")
	if got, _ := m.Text(rec.ID); got != "explicit" {
		t.Fatalf("explicit buffer must win: %q", got)
	}
}

func TestDetachTextSourceFallsBackToDisk(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	sink.files[rec.ID] = "typed"
	m.SetTextSource(rec.ID, func() string { return "live" })
	m.DetachTextSource(rec.ID)
	if got, _ := m.Text(rec.ID); got != "typed" {
		t.Fatalf("detached console must re-read disk: %q", got)
	}
	m.TouchVersion(rec.ID, 1) // 没有来源时不应置脏
	if m.Dirty(rec.ID) {
		t.Fatal("version bumps without a source must not mark dirty")
	}
}

func TestFlushStillUsesLiveSource(t *testing.T) {
	sink := newFakeSink()
	_, _, rec, m := newMgr(t, sink)
	live := "v1"
	m.SetTextSource(rec.ID, func() string { return live })
	m.TouchVersion(rec.ID, 1)
	live = "v2"
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := sink.text(rec.ID); got != "v2" {
		t.Fatalf("flush must write the live editor body: %q", got)
	}
}
