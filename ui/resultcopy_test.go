package ui

import (
	"strings"
	"testing"
	"time"
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
	got := rowsTSV(r, []int{0, 1}, []int{0, 1}, true)
	want := "id\tname\n1\talice\n2\t\"bob\tx\""
	if got != want {
		t.Fatalf("rowsTSV:\n%q\nwant\n%q", got, want)
	}
	// 只取第二行第二列
	if got := rowsTSV(r, []int{1}, []int{1}, false); got != "\"bob\tx\"" {
		t.Fatalf("subset: %q", got)
	}
}

func TestInsertSQL(t *testing.T) {
	cols := []string{"id", "name", "note"}
	row := []any{int64(1), "O'Reilly", nil}
	got := insertSQL("sqlite", "users", cols, row)
	want := `INSERT INTO "users" ("id", "name", "note") VALUES (1, 'O''Reilly', NULL);`
	if got != want {
		t.Fatalf("sqlite insert:\n%q\nwant\n%q", got, want)
	}
	got = insertSQL("mysql", "users", cols, row)
	want = "INSERT INTO `users` (`id`, `name`, `note`) VALUES (1, 'O''Reilly', NULL);"
	if got != want {
		t.Fatalf("mysql insert:\n%q\nwant\n%q", got, want)
	}
	// 字节文本按字符串转义；换行保留在引号内
	got = insertSQL("sqlite", "t", []string{"c"}, []any{[]byte("a\nb")})
	if !strings.Contains(got, "'a\nb'") {
		t.Fatalf("bytes insert: %q", got)
	}
}

func TestFormatCellValueJSON(t *testing.T) {
	text, isJSON := formatCellValue([]byte(`{"a":1}`))
	if !isJSON || !strings.Contains(text, "\n") || !strings.Contains(text, `"a": 1`) {
		t.Fatalf("json formatted: %q isJSON=%v", text, isJSON)
	}
	text, isJSON = formatCellValue("not json")
	if isJSON || text != "not json" {
		t.Fatalf("plain: %q %v", text, isJSON)
	}
}

func TestInsertSQLKeepsJSONStructure(t *testing.T) {
	payload := `{"path":"C:\\tmp","quote":"say \"hi\"","name":"O'Reilly"}`
	got := insertSQL("mysql", "meeting_case3", []string{"payload"}, []any{payload})
	if !strings.Contains(got, `C:\\\\tmp`) || !strings.Contains(got, `say \\"hi\\"`) ||
		!strings.Contains(got, `O''Reilly`) {
		t.Fatalf("mysql JSON escaping: %s", got)
	}
	got = insertSQL("postgres", "meeting_case3", []string{"payload"}, []any{payload})
	if !strings.Contains(got, `C:\\tmp`) || !strings.Contains(got, `say \"hi\"`) {
		t.Fatalf("postgres JSON escaping: %s", got)
	}
}
