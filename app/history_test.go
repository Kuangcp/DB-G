package app

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryPerConnection(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir)
	for i := 0; i < 3; i++ {
		if err := h.Add(HistoryEntry{ConnID: "a", ConnName: "A", SQL: "select a", OK: true, Rows: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Add(HistoryEntry{ConnID: "b", SQL: "select b", OK: true}); err != nil {
		t.Fatal(err)
	}
	if got := h.Path("a"); got != filepath.Join(dir, "a.jsonl") {
		t.Fatalf("path: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.jsonl")); err != nil {
		t.Fatal(err)
	}
	list := h.List("a", 10)
	if len(list) != 3 || list[0].SQL != "select a" || !list[0].OK {
		t.Fatalf("a list: %+v", list)
	}
	if list := h.List("b", 10); len(list) != 1 || list[0].SQL != "select b" {
		t.Fatalf("b list: %+v", list)
	}
	// 重开（模拟重启）仍能读到。
	re := OpenHistory(dir)
	if got := re.List("a", 10); len(got) != 3 {
		t.Fatalf("reload: %d", len(got))
	}
	if got := re.Path(""); got != filepath.Join(dir, "default.jsonl") {
		t.Fatalf("empty conn: %q", got)
	}
}

func TestHistoryNewestFirstAndLimit(t *testing.T) {
	h := OpenHistory(t.TempDir())
	for _, sql := range []string{"a", "b", "c"} {
		if err := h.Add(HistoryEntry{ConnID: "c", SQL: sql, OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	list := h.List("c", 2)
	if len(list) != 2 || list[0].SQL != "c" || list[1].SQL != "b" {
		t.Fatalf("list: %+v", list)
	}
}

func TestHistorySkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "c.jsonl"), []byte("{\"sql\":\"ok\"}\nnot json\n{\"sql\":\"ok2\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := OpenHistory(dir)
	if got := h.List("c", 10); len(got) != 2 {
		t.Fatalf("bad lines must be skipped: %+v", got)
	}
}

func historyLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		n++
	}
	return n
}

// 追加到 6000 条时裁剪回 5000，之后到 5999 都不再写文件。
func TestHistoryTrimsLazily(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir)
	for i := 0; i < HistoryTrimAt-1; i++ {
		if err := h.Add(HistoryEntry{ConnID: "c", SQL: "select 1", OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	path := h.Path("c")
	if n := historyLines(t, path); n != HistoryTrimAt-1 {
		t.Fatalf("before trim: %d", n)
	}
	if err := h.Add(HistoryEntry{ConnID: "c", SQL: "select 1", OK: true}); err != nil {
		t.Fatal(err)
	}
	if n := historyLines(t, path); n != HistoryKeep {
		t.Fatalf("trim at %d: %d", HistoryTrimAt, n)
	}
	if list := h.List("c", 0); len(list) != HistoryKeep {
		t.Fatalf("kept entries: %d", len(list))
	}
	// 再追加一条不触发裁剪。
	if err := h.Add(HistoryEntry{ConnID: "c", SQL: "select 2", OK: true}); err != nil {
		t.Fatal(err)
	}
	if n := historyLines(t, path); n != HistoryKeep+1 {
		t.Fatalf("after one more: %d", n)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("perms: %v", info.Mode().Perm())
	}
}

func TestHistoryMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "history.jsonl")
	body := `{"connID":"a","sql":"one","ok":true}
{"connID":"b","sql":"two","ok":true}
{"connID":"a","sql":"three","ok":true}
`
	if err := os.WriteFile(old, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h := OpenHistory(filepath.Join(dir, "history"))
	if err := h.MigrateLegacy(old); err != nil {
		t.Fatal(err)
	}
	if got := h.List("a", 10); len(got) != 2 || got[0].SQL != "three" || got[1].SQL != "one" {
		t.Fatalf("a: %+v", got)
	}
	if got := h.List("b", 10); len(got) != 1 || got[0].SQL != "two" {
		t.Fatalf("b: %+v", got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("旧文件应改名: %v", err)
	}
	if _, err := os.Stat(old + ".migrated"); err != nil {
		t.Fatalf("缺少 .migrated: %v", err)
	}
	// 再调一次不应重复导入。
	if err := h.MigrateLegacy(old); err != nil {
		t.Fatal(err)
	}
	if got := h.List("a", 10); len(got) != 2 {
		t.Fatalf("重复迁移: %d", len(got))
	}
}
