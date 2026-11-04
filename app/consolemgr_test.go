package app

import (
	"errors"
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
