package app

import (
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
