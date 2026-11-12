package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestCellStringFullValue(t *testing.T) {
	long := strings.Repeat("x", 600)
	cases := []struct {
		v    any
		want string
	}{
		{nil, "NULL"},
		{"hi", "hi"},
		{[]byte("你好"), "你好"},
		{[]byte{0xff, 0xfe}, "[2 bytes]"},
		{int64(42), "42"},
		{3.5, "3.5"},
		{true, "true"},
		{time.Date(2026, 11, 5, 1, 2, 3, 0, time.UTC), "2026-11-05T01:02:03Z"},
		{long, long}, // 复制不截断
	}
	for _, c := range cases {
		if got := cellString(c.v); got != c.want {
			t.Fatalf("cellString(%v) = %q want %q", c.v, got, c.want)
		}
	}
}

func TestTSVFieldQuoting(t *testing.T) {
	cases := map[string]string{
		"plain":    "plain",
		"a\tb":     "\"a\tb\"",
		"a\nb":     "\"a\nb\"",
		`say "hi"`: `"say ""hi"""`,
	}
	for in, want := range cases {
		if got := tsvField(in); got != want {
			t.Fatalf("tsvField(%q) = %q want %q", in, got, want)
		}
	}
}

func TestRowsTSV(t *testing.T) {
	r := resultWith([]string{"id", "name"}, []any{int64(1), "alice"}, []any{int64(2), "bob\tx"})
	got := rowsTSVFor("", r, []int{0, 1}, []int{0, 1}, true)
	want := "id\tname\n1\talice\n2\t\"bob\tx\""
	if got != want {
		t.Fatalf("rowsTSV:\n%q\nwant\n%q", got, want)
	}
	// 只取第二行第二列
	if got := rowsTSVFor("", r, []int{1}, []int{1}, false); got != "\"bob\tx\"" {
		t.Fatalf("subset: %q", got)
	}
}

func TestInsertSQL(t *testing.T) {
	cols := []string{"id", "name", "note"}
	row := []any{int64(1), "O'Reilly", nil}
	got := insertSQLFor("sqlite", "users", cols, nil, row)
	want := `INSERT INTO "users" ("id", "name", "note") VALUES (1, 'O''Reilly', NULL);`
	if got != want {
		t.Fatalf("sqlite insert:\n%q\nwant\n%q", got, want)
	}
	got = insertSQLFor("mysql", "users", cols, nil, row)
	want = "INSERT INTO `users` (`id`, `name`, `note`) VALUES (1, 'O''Reilly', NULL);"
	if got != want {
		t.Fatalf("mysql insert:\n%q\nwant\n%q", got, want)
	}
	// 字节文本按字符串转义；换行保留在引号内
	got = insertSQLFor("sqlite", "t", []string{"c"}, nil, []any{[]byte("a\nb")})
	if !strings.Contains(got, "'a\nb'") {
		t.Fatalf("bytes insert: %q", got)
	}
}

func TestFormatCellValueJSON(t *testing.T) {
	text, isJSON := formatCellText(cellString([]byte(`{"a":1}`)))
	if !isJSON || !strings.Contains(text, "\n") || !strings.Contains(text, `"a": 1`) {
		t.Fatalf("json formatted: %q isJSON=%v", text, isJSON)
	}
	text, isJSON = formatCellText("not json")
	if isJSON || text != "not json" {
		t.Fatalf("plain: %q %v", text, isJSON)
	}
}

func TestInsertSQLKeepsJSONStructure(t *testing.T) {
	payload := `{"path":"C:\\tmp","quote":"say \"hi\"","name":"O'Reilly"}`
	got := insertSQLFor("mysql", "meeting_case3", []string{"payload"}, nil, []any{payload})
	if !strings.Contains(got, `C:\\\\tmp`) || !strings.Contains(got, `say \\"hi\\"`) ||
		!strings.Contains(got, `O''Reilly`) {
		t.Fatalf("mysql JSON escaping: %s", got)
	}
	got = insertSQLFor("postgres", "meeting_case3", []string{"payload"}, nil, []any{payload})
	if !strings.Contains(got, `C:\\tmp`) || !strings.Contains(got, `say \"hi\"`) {
		t.Fatalf("postgres JSON escaping: %s", got)
	}
}

func TestCellStringForMySQLDateTime(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	ts := time.Date(2030, 11, 11, 18, 28, 47, 0, zone)
	r := &engine.QueryResult{
		IsQuery:     true,
		Columns:     []string{"created_at", "updated_at", "day"},
		ColumnTypes: []string{"DATETIME", "TIMESTAMP", "DATE"},
		Rows:        [][]any{{ts, ts, ts}},
	}
	if got := cellStringFor("mysql", r, 0, ts); got != "2030-11-11 18:28:47" {
		t.Fatalf("DATETIME = %q", got)
	}
	if got := cellStringFor("mysql", r, 1, ts); got != "2030-11-11 18:28:47" {
		t.Fatalf("TIMESTAMP = %q", got)
	}
	if got := cellStringFor("mysql", r, 2, ts); got != "2030-11-11T18:28:47+08:00" {
		t.Fatalf("DATE must keep RFC3339, got %q", got)
	}
	if got := cellStringFor("sqlite", r, 0, ts); got != "2030-11-11T18:28:47+08:00" {
		t.Fatalf("non-mysql must keep RFC3339, got %q", got)
	}
	if got := cellStringFor("mysql", r, 0, nil); got != "NULL" {
		t.Fatalf("nil = %q", got)
	}
	if got := cellTextFor("mysql", r, 0, ts); got != "2030-11-11 18:28:47" {
		t.Fatalf("table text = %q", got)
	}
}

func TestRowsTSVForMySQLDateTime(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	ts := time.Date(2030, 11, 11, 18, 28, 47, 0, zone)
	r := &engine.QueryResult{
		IsQuery:     true,
		Columns:     []string{"id", "created_at"},
		ColumnTypes: []string{"BIGINT", "DATETIME"},
		Rows:        [][]any{{int64(1), ts}},
	}
	if got, want := rowsTSVFor("mysql", r, []int{0}, []int{0, 1}, true),
		"id\tcreated_at\n1\t2030-11-11 18:28:47"; got != want {
		t.Fatalf("TSV:\n got %q\nwant %q", got, want)
	}
	got := blockCSV("mysql", r, []int{0}, []int{1}, false)
	if got != "2030-11-11 18:28:47" {
		t.Fatalf("CSV = %q", got)
	}
	got = insertSQLFor("mysql", "t", r.Columns, r.ColumnTypes, r.Rows[0])
	if !strings.Contains(got, "'2030-11-11 18:28:47'") {
		t.Fatalf("typed insert = %q", got)
	}
}

func TestCopyLongCellKeepsFullText(t *testing.T) {
	long := strings.Repeat("甲", 100_000)
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"raw_result_json"},
		Rows:    [][]any{{[]byte(long)}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.resultRow = 0
	a.resultState.Cells = ui.ColumnRange{Cursor: "raw_result_json", Anchor: "raw_result_json"}
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != long {
		t.Fatalf("clipboard length = %d, want %d", len(got), len(long))
	}
}

func TestCellTextDisplayBoundedCopyFull(t *testing.T) {
	long := strings.Repeat("甲", 100_000)
	r := &engine.QueryResult{IsQuery: true, Columns: []string{"raw"}, Rows: [][]any{{[]byte(long)}}}
	display := cellTextFor("", r, 0, r.Rows[0][0])
	if n := len([]rune(display)); n > displayCellRunes+1 {
		t.Fatalf("display length = %d runes, want <= %d", n, displayCellRunes+1)
	}
	if !strings.HasSuffix(display, "…") {
		t.Fatalf("long display must end with ellipsis: %q", display)
	}
	if full := cellStringFor("", r, 0, r.Rows[0][0]); full != long {
		t.Fatalf("copy must stay full: got %d runes", len([]rune(full)))
	}
	bin := append([]byte{0}, bytes.Repeat([]byte("x"), displayCellBytes+100)...)
	if got := cellTextDisplay(bin); got != fmt.Sprintf("[%d bytes]", len(bin)) {
		t.Fatalf("binary display = %q", got)
	}
}
