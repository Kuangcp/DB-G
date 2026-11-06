package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

func logFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".log") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestLoggerWritesReadableLine(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(LevelInfo, "exec", "ok", "dur_ms", 3, "rows", 500, "err", "boom boom")
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} INFO  \[exec\] ok dur_ms=3 rows=500 err="boom boom"$`)
	if !re.MatchString(line) {
		t.Fatalf("log line:\n%s", line)
	}
	// 一次启动一个文件：<root>/<YYYY-MM>/<YYYY-MM-DD>_0.log
	rel, err := filepath.Rel(dir, l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}/\d{4}-\d{2}-\d{2}_0\.log$`).MatchString(rel) {
		t.Fatalf("log path: %s", rel)
	}
}

func TestLoggerLevelFilter(t *testing.T) {
	l, _ := NewLogger(t.TempDir())
	defer l.Close()
	l.SetLevel(LevelWarn)
	l.Log(LevelInfo, "t", "hidden")
	l.Log(LevelError, "t", "shown")
	data, _ := os.ReadFile(l.Path())
	if strings.Contains(string(data), "hidden") {
		t.Fatalf("info must be filtered: %s", data)
	}
	if !strings.Contains(string(data), "ERROR [t] shown") {
		t.Fatalf("error must be written: %s", data)
	}
}

func TestLoggerOneFilePerStartup(t *testing.T) {
	dir := t.TempDir()
	l1, err := NewLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := NewLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if l1.Path() == l2.Path() {
		t.Fatalf("two startups must not share a file: %s", l1.Path())
	}
	l1.Log(LevelInfo, "t", "one")
	l2.Log(LevelInfo, "t", "two")
	d1, _ := os.ReadFile(l1.Path())
	d2, _ := os.ReadFile(l2.Path())
	if !strings.Contains(string(d1), "one") || strings.Contains(string(d1), "two") {
		t.Fatalf("first file: %s", d1)
	}
	if !strings.Contains(string(d2), "two") || strings.Contains(string(d2), "one") {
		t.Fatalf("second file: %s", d2)
	}
	if !strings.HasSuffix(l1.Path(), "_0.log") || !strings.HasSuffix(l2.Path(), "_1.log") {
		t.Fatalf("startup indexes: %s %s", l1.Path(), l2.Path())
	}
}

func TestLoggerKeepsOneFilePerStartupAcrossDays(t *testing.T) {
	dir := t.TempDir()
	l, _ := NewLogger(dir)
	defer l.Close()
	l.now = func() time.Time { return time.Date(2026, 11, 5, 23, 59, 0, 0, time.Local) }
	l.Log(LevelInfo, "t", "first")
	first := l.Path()
	l.now = func() time.Time { return time.Date(2026, 11, 6, 0, 1, 0, 0, time.Local) }
	l.Log(LevelInfo, "t", "second")
	if l.Path() != first {
		t.Fatalf("one startup must keep one file, got %s then %s", first, l.Path())
	}
	data, _ := os.ReadFile(first)
	if !strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Fatalf("log content: %s", data)
	}
}

func TestLogPackageSafeWhenUninitialized(t *testing.T) {
	CloseLogs()
	defer CloseLogs()
	LogInfo("t", "noop")
	LogWarn("t", "noop")
	LogError("t", "noop")
}

func TestLogsToFileAfterInit(t *testing.T) {
	dir := t.TempDir()
	if err := InitLogs(dir); err != nil {
		t.Fatal(err)
	}
	defer CloseLogs()
	LogInfo("startup", "hello", "n", 1)
	LogError("conn", "failed", "err", "boom")
	files := logFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected one log file, got %v", files)
	}
	data, _ := os.ReadFile(files[0])
	text := string(data)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "failed") ||
		!strings.Contains(text, "err=boom") || strings.HasPrefix(strings.TrimSpace(text), "{") {
		t.Fatalf("log content: %s", text)
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log perms = %v", info.Mode().Perm())
	}
}

func TestRedactDSN(t *testing.T) {
	cases := map[string]string{
		"u:secret@tcp(h:3306)/db":       "u:***@tcp(h:3306)/db",
		"host=h user=u password=s db=d": "host=h user=u password=*** db=d",
		"plain":                         "plain",
	}
	for in, want := range cases {
		if got := RedactDSN(in); got != want {
			t.Fatalf("RedactDSN(%q) = %q want %q", in, got, want)
		}
	}
}
