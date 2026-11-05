package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestRestoreWorkspaceAndConsole(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	s := app.LoadStore(storePath)
	p := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := s.Add(p); err != nil {
		t.Fatal(err)
	}
	prof := s.List()[0]
	m := app.NewConsoleManager(s, app.ConsoleFiles{Dir: s.ConsolesDir()})
	rec, err := m.Create(prof.ID, "上次的")
	if err != nil {
		t.Fatal(err)
	}
	m.SetText(rec.ID, "SELECT 100")
	m.SetCaret(rec.ID, 7, 7)
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	m.FlushCarets()

	a := newRootWithStore(storePath)
	if a.activeConsole != rec.ID {
		t.Fatalf("active console: %q", a.activeConsole)
	}
	if got := a.activeText(); got != "SELECT 100" {
		t.Fatalf("restored text: %q", got)
	}
	if cs, ce := a.code.CaretRunes(); cs != 7 || ce != 7 {
		t.Fatalf("restored caret: %d,%d", cs, ce)
	}
	if _, ok := a.store.GetConsole(rec.ID); !ok {
		t.Fatal("console metadata must exist")
	}
}

func TestRestoreFallsBackOnInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	raw := `{"conns":[],"consoles":[],"workspaces":[{"id":"w1","name":"默认","members":[],"lastActiveConsoleID":"gone"}],"activeWorkspaceID":"nowhere"}`
	if err := os.WriteFile(storePath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newRootWithStore(storePath)
	if ws, ok := a.consoleMgr().ActiveWorkspace(); !ok || ws.ID != "w1" {
		t.Fatalf("fallback workspace: %+v %v", ws, ok)
	}
	if _, ok := a.consoleMgr().Active(); ok {
		t.Fatal("no console must be active")
	}
	if a.activeText() != "" {
		t.Fatalf("empty buffer expected: %q", a.activeText())
	}
	if a.activeConsole != "" {
		t.Fatalf("active console must be empty: %q", a.activeConsole)
	}
}

func TestEmptyStoreDoesNotWriteOnStartup(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	a := newRootWithStore(storePath)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup must not write config: %v", err)
	}
	if !tt.HasText("在左侧数据源下新建或打开控制台") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestFlushAllWritesDirtyConsoles(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("SELECT 77")
	a.syncActiveBuffer()
	a.FlushAll()
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir()}.Read(rec.ID)
	if err != nil || text != "SELECT 77" {
		t.Fatalf("flush all: %q %v", text, err)
	}
}
