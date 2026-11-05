package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesJSONLine(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(LevelInfo, "exec", "ok", "dur_ms", 3, "rows", 500)
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("bad line %q: %v", data, err)
	}
	if got["level"] != "info" || got["tag"] != "exec" || got["msg"] != "ok" {
		t.Fatalf("line: %v", got)
	}
	kv, _ := got["kv"].(map[string]any)
	if kv["rows"] != float64(500) {
		t.Fatalf("kv: %v", kv)
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
	if !strings.Contains(string(data), "shown") {
		t.Fatalf("error must be written: %s", data)
	}
}

func TestLoggerRotatesAtMaxSize(t *testing.T) {
	dir := t.TempDir()
	l, _ := NewLogger(dir)
	defer l.Close()
	l.maxSize = 200
	for i := 0; i < 20; i++ {
		l.Log(LevelInfo, "t", "padding padding padding padding")
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) < 2 {
		t.Fatalf("expected a rotated file, got %v", names)
	}
	if !strings.Contains(strings.Join(names, ","), ".1.log") {
		t.Fatalf("rotation name missing: %v", names)
	}
}

func TestLoggerDayRollover(t *testing.T) {
	dir := t.TempDir()
	l, _ := NewLogger(dir)
	defer l.Close()
	day1 := time.Date(2026, 11, 5, 23, 59, 0, 0, time.Local)
	day2 := day1.Add(2 * time.Minute)
	l.now = func() time.Time { return day1 }
	l.Log(LevelInfo, "t", "first")
	first := l.Path()
	l.now = func() time.Time { return day2 }
	l.Log(LevelInfo, "t", "second")
	if l.Path() == first {
		t.Fatalf("day rollover must switch files: %s", l.Path())
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
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected one log file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(data), "hello") || !strings.Contains(string(data), "boom") {
		t.Fatalf("log content: %s", data)
	}
	info, _ := entries[0].Info()
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
