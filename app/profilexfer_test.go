package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportProfilesWithoutPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conns.json")
	conns := []ConnProfile{
		{ID: "a", Name: "local", Kind: "sqlite", Path: "/tmp/x.db"},
		{ID: "b", Name: "m", Kind: "mysql", Host: "h", Port: 3306, User: "u", Password: "secret"},
	}
	n, err := ExportProfiles(path, conns, false)
	if err != nil || n != 2 {
		t.Fatalf("export: n=%d err=%v", n, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("password leaked: %s", raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("perms: %v", info.Mode().Perm())
	}
}

func TestImportProfilesDedupesAndRemapsIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conns.json")
	exported := []ConnProfile{
		{ID: "x", Name: "local", Kind: "sqlite", Path: "/tmp/x.db"},
		{ID: "y", Name: "m", Kind: "mysql", Host: "h", Port: 3306, User: "u"},
	}
	if _, err := ExportProfiles(path, exported, true); err != nil {
		t.Fatal(err)
	}
	existing := []ConnProfile{{ID: "x", Name: "local", Kind: "sqlite", Path: "/tmp/x.db"}}
	added, err := ImportProfiles(path, existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].Name != "m" {
		t.Fatalf("added: %+v", added)
	}
	if added[0].ID != "y" {
		t.Fatalf("ids only remap on conflict, got %q", added[0].ID)
	}
	// 再导入一次：全部重复 → 不新增
	again, err := ImportProfiles(path, append(existing, added...))
	if err != nil || len(again) != 0 {
		t.Fatalf("second import: %+v err=%v", again, err)
	}
}

func TestImportRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportProfiles(path, nil); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestImportRemapsConflictingID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conns.json")
	if _, err := ExportProfiles(path, []ConnProfile{
		{ID: "same", Name: "db2", Kind: "sqlite", Path: "/tmp/y.db"},
	}, true); err != nil {
		t.Fatal(err)
	}
	existing := []ConnProfile{{ID: "same", Name: "db1", Kind: "sqlite", Path: "/tmp/x.db"}}
	added, err := ImportProfiles(path, existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].ID == "same" || added[0].ID == "" {
		t.Fatalf("conflicting id must be remapped: %+v", added)
	}
}
