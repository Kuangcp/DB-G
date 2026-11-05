package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	h := OpenHistory(path)
	for i := 0; i < 3; i++ {
		if err := h.Add(HistoryEntry{SQL: "select 1", ConnName: "c", OK: true, Rows: 1}); err != nil {
			t.Fatal(err)
		}
	}
	re := OpenHistory(path)
	list := re.List(10)
	if len(list) != 3 {
		t.Fatalf("loaded %d", len(list))
	}
	if list[0].SQL != "select 1" || !list[0].OK {
		t.Fatalf("entry: %+v", list[0])
	}
	if list[0].Time == 0 {
		t.Fatal("time must be filled")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perms: %v", info.Mode().Perm())
	}
}

func TestHistoryNewestFirstAndLimit(t *testing.T) {
	h := OpenHistory(filepath.Join(t.TempDir(), "h.jsonl"))
	for _, sql := range []string{"a", "b", "c"} {
		if err := h.Add(HistoryEntry{SQL: sql, OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	list := h.List(2)
	if len(list) != 2 || list[0].SQL != "c" || list[1].SQL != "b" {
		t.Fatalf("list: %+v", list)
	}
}

func TestHistorySkipsBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.jsonl")
	if err := os.WriteFile(path, []byte("{\"sql\":\"ok\"}\nnot json\n{\"sql\":\"ok2\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := OpenHistory(path)
	if got := h.List(10); len(got) != 2 {
		t.Fatalf("bad lines must be skipped: %+v", got)
	}
}

func TestHistoryRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.jsonl")
	h := OpenHistory(path)
	big := make([]byte, 1024)
	for i := range big {
		big[i] = 'x'
	}
	for i := 0; i < 6000; i++ { // ~6MB 总写入
		if err := h.Add(HistoryEntry{SQL: string(big), OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal("history must rotate at 5MB:", err)
	}
}
