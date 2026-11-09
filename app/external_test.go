package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func externalFixture(t *testing.T, content string) (*ConnStore, ConsoleFiles, ConsoleRecord, string) {
	t.Helper()
	dir := t.TempDir()
	store := LoadStore(filepath.Join(dir, "connections.json"))
	prof := mustAdd(t, store, "本地", filepath.Join(dir, "db.sqlite"))
	path := filepath.Join(dir, "query.sql")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	rec, err := store.CreateExternalConsole(prof.ID, "query", path)
	if err != nil {
		t.Fatal(err)
	}
	return store, ConsoleFiles{Dir: filepath.Join(dir, "consoles"), Store: store}, rec, path
}

func TestReadExternalSQLStripsBOMAndRejectsBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.sql")
	if err := os.WriteFile(path, []byte("\xEF\xBB\xBFselect 1;\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := ReadExternalSQL(path)
	if err != nil || text != "select 1;\r\n" {
		t.Fatalf("BOM 应去掉: %q %v", text, err)
	}

	bad := filepath.Join(dir, "bad.sql")
	if err := os.WriteFile(bad, []byte{0xFF, 0xFE, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExternalSQL(bad); !errors.Is(err, ErrNotTextSQL) {
		t.Fatalf("非法 UTF-8/NUL 应拒绝: %v", err)
	}

	big := filepath.Join(dir, "big.sql")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", MaxExternalSQLSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExternalSQL(big); !errors.Is(err, ErrExternalTooBig) {
		t.Fatalf("超限应拒绝: %v", err)
	}
}

func TestConsoleFilesExternalPreservesBOMAndEOL(t *testing.T) {
	_, files, rec, path := externalFixture(t, "\xEF\xBB\xBFselect 1;\r\nselect 2;\r\n")
	text, err := files.Read(rec.ID)
	if err != nil || text != "select 1;\r\nselect 2;\r\n" {
		t.Fatalf("外部读取: %q %v", text, err)
	}
	if err := files.Write(rec.ID, "select 3;\nselect 4;\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\xEF\xBB\xBFselect 3;\r\nselect 4;\r\n" {
		t.Fatalf("BOM/CRLF 应保留: %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("权限应保留: %v", info.Mode().Perm())
	}
}

func TestConsoleFilesExternalReadMissingReturnsNotExist(t *testing.T) {
	_, files, rec, path := externalFixture(t, "select 1;\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Read(rec.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失应返回 ErrNotExist: %v", err)
	}
	if err := files.Delete(rec.ID); err != nil {
		t.Fatalf("删除绑定不应碰用户文件: %v", err)
	}
}

func TestCheckExternalSQL(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ok.sql")
	if err := os.WriteFile(good, []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CheckExternalSQL(good)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(good)
	if got != want {
		t.Fatalf("路径: %q want %q", got, want)
	}
	if _, err := CheckExternalSQL(filepath.Join(dir, "note.txt")); !errors.Is(err, ErrNotSQLFile) {
		t.Fatalf("非 .sql 应拒绝: %v", err)
	}
	if _, err := CheckExternalSQL(filepath.Join(dir, "missing.sql")); err == nil {
		t.Fatal("缺失文件应报错")
	}
	big := filepath.Join(dir, "big.sql")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", MaxExternalSQLSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckExternalSQL(big); !errors.Is(err, ErrExternalTooBig) {
		t.Fatalf("超限应拒绝: %v", err)
	}
}
