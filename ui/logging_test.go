package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func initTestLogs(t *testing.T) string {
	t.Helper()
	t.Setenv("DBG_LOG_STDERR", "0")
	dir := t.TempDir()
	if err := app.InitLogs(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.CloseLogs)
	return dir
}

func readLogs(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".log") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestOpenConnLogsWithoutPassword(t *testing.T) {
	dir := initTestLogs(t)
	a := newConsoleRoot(t)
	prof := app.ConnProfile{Name: "本地", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(prof); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	logs := readLogs(t, dir)
	if !strings.Contains(logs, `[conn]`) {
		t.Fatalf("conn log missing: %s", logs)
	}
	if !strings.Contains(logs, stored.Path) {
		t.Fatalf("conn log should mention the database path: %s", logs)
	}
}

func TestExecLogsSQLAndDuration(t *testing.T) {
	dir := initTestLogs(t)
	a := openForSelection(t)
	a.setActiveText("SELECT 1 AS x")
	a.setSelection(0, len([]rune("SELECT 1 AS x")))
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	logs := readLogs(t, dir)
	if !strings.Contains(logs, `[exec]`) || !strings.Contains(logs, "SELECT 1 AS x") {
		t.Fatalf("exec log missing sql/tag: %s", logs)
	}
	// 成功执行不再打印结果统计（rows/affected/truncated）。
	for _, bad := range []string{"rows=", "affected=", "truncated="} {
		if strings.Contains(logs, bad) {
			t.Fatalf("exec log must not contain query results (%s): %s", bad, logs)
		}
	}
}

func TestTruncateLogSQL(t *testing.T) {
	short := "SELECT 1"
	if got := truncateLogSQL(short); got != short {
		t.Fatalf("short sql changed: %q", got)
	}
	long := strings.Repeat("字", 250)
	got := truncateLogSQL(long)
	if !strings.HasSuffix(got, "...") || len([]rune(strings.TrimSuffix(got, "..."))) != 200 {
		t.Fatalf("truncate: %d runes, suffix ok=%v", len([]rune(got)), strings.HasSuffix(got, "..."))
	}
}

func TestGuardViewRecoversPanic(t *testing.T) {
	dir := initTestLogs(t)
	a := newConsoleRoot(t)
	a.guardView(func() { panic("boom") })
	if !strings.Contains(a.consoleErr, "内部错误") {
		t.Fatalf("panic must surface as UI error, got %q", a.consoleErr)
	}
	logs := readLogs(t, dir)
	if !strings.Contains(logs, "boom") || !strings.Contains(logs, "stack") {
		t.Fatalf("panic not logged with stack: %s", logs)
	}
}
