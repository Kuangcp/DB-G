package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestConsoleFilesReadMissing(t *testing.T) {
	f := ConsoleFiles{Dir: t.TempDir()}
	text, err := f.Read("deadbeef")
	if err != nil || text != "" {
		t.Fatalf("missing read: %q %v", text, err)
	}
	if f.Exists("deadbeef") {
		t.Fatal("must not exist")
	}
}

func TestConsoleFilesWriteReplacesAtomically(t *testing.T) {
	f := ConsoleFiles{Dir: filepath.Join(t.TempDir(), "consoles")}
	if err := f.Write("c1", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Write("c1", "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	text, err := f.Read("c1")
	if err != nil || text != "SELECT 2" {
		t.Fatalf("read: %q %v", text, err)
	}
	if !f.Exists("c1") {
		t.Fatal("must exist")
	}
	if err := f.Delete("c1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("c1"); err != nil {
		t.Fatal("delete must be idempotent:", err)
	}
	if f.Exists("c1") {
		t.Fatal("must be gone")
	}
}

func TestConsoleFilesPath(t *testing.T) {
	f := ConsoleFiles{Dir: "/tmp/x"}
	if got := f.Path("abc"); got != "/tmp/x/abc.sql" {
		t.Fatalf("path: %q", got)
	}
}

// 外部控制台：写回用户文件、保留权限、删除控制台绝不删用户文件。
func TestExternalConsoleFiles(t *testing.T) {
	dir := t.TempDir()
	store := LoadStore(filepath.Join(dir, "connections.json"))
	db := filepath.Join(dir, "x.db")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	prof := ConnProfile{Name: "p", Kind: "sqlite", Path: db}
	if err := store.Add(prof); err != nil {
		t.Fatal(err)
	}
	p := store.List()[0]
	ext := filepath.Join(dir, "note.sql")
	if err := os.WriteFile(ext, []byte("SELECT 1"), 0o640); err != nil {
		t.Fatal(err)
	}
	rec, err := store.CreateExternalConsole(p.ID, "", ext)
	if err != nil {
		t.Fatal(err)
	}
	files := ConsoleFiles{Dir: store.ConsolesDir(), Store: store}
	got, err := files.Read(rec.ID)
	if err != nil || got != "SELECT 1" {
		t.Fatalf("read: %q %v", got, err)
	}
	if err := files.Write(rec.ID, "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ext)
	if err != nil || string(data) != "SELECT 2" {
		t.Fatalf("write: %q %v", data, err)
	}
	if info, _ := os.Stat(ext); info.Mode().Perm() != 0o640 {
		t.Fatalf("外部文件权限应保留: %v", info.Mode().Perm())
	}
	if err := files.Delete(rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ext); err != nil {
		t.Fatalf("删除控制台不能删外部文件: %v", err)
	}
	if err := os.Remove(ext); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Read(rec.ID); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("外部文件缺失应返回 ErrNotExist: %v", err)
	}
}

// 外部文件写前一致性：磁盘被改后普通 Flush 拒绝覆盖，FlushForce 才允许。
func TestExternalFlushRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	store := LoadStore(filepath.Join(dir, "connections.json"))
	db := filepath.Join(dir, "x.db")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	prof := ConnProfile{Name: "p", Kind: "sqlite", Path: db}
	if err := store.Add(prof); err != nil {
		t.Fatal(err)
	}
	p := store.List()[0]
	ext := filepath.Join(dir, "note.sql")
	if err := os.WriteFile(ext, []byte("SELECT 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := store.CreateExternalConsole(p.ID, "", ext)
	if err != nil {
		t.Fatal(err)
	}
	m := NewConsoleManager(store, ConsoleFiles{Dir: store.ConsolesDir(), Store: store})
	if _, ok := m.Text(rec.ID); !ok {
		t.Fatal("text")
	}
	m.SetText(rec.ID, "local")
	if err := m.Flush(rec.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(ext); string(data) != "local" {
		t.Fatalf("first flush: %q", data)
	}
	// 外部编辑器改写文件。
	if err := os.WriteFile(ext, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.SetText(rec.ID, "mine")
	if err := m.Flush(rec.ID); !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("应拒绝覆盖: %v", err)
	}
	if data, _ := os.ReadFile(ext); string(data) != "outside" {
		t.Fatalf("外部内容被覆盖: %q", data)
	}
	if err := m.FlushForce(rec.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(ext); string(data) != "mine" {
		t.Fatalf("force flush: %q", data)
	}
}

// 旧命名 <id>.sql 启动时迁移为 <connectionID>-<id>.sql。
func TestConsoleFilesMigrateOldNames(t *testing.T) {
	dir := t.TempDir()
	store := LoadStore(filepath.Join(dir, "connections.json"))
	db := filepath.Join(dir, "x.db")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ConnProfile{Name: "p", Kind: "sqlite", Path: db}); err != nil {
		t.Fatal(err)
	}
	p := store.List()[0]
	rec, err := store.CreateConsole(p.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	files := ConsoleFiles{Dir: store.ConsolesDir(), Store: store}
	want := filepath.Join(store.ConsolesDir(), p.ID+"-"+rec.ID+".sql")
	if got := files.Path(rec.ID); got != want {
		t.Fatalf("path: %q, want %q", got, want)
	}
	old := filepath.Join(store.ConsolesDir(), rec.ID+".sql")
	if err := os.MkdirAll(store.ConsolesDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("SELECT old"), 0o600); err != nil {
		t.Fatal(err)
	}
	files.Migrate()
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("旧文件应被改名: %v", err)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "SELECT old" {
		t.Fatalf("迁移内容: %q %v", data, err)
	}
}
