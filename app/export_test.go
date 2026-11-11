package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExportCSVQuoting(t *testing.T) {
	var buf bytes.Buffer
	w := NewExportWriter(&buf, "csv", "sqlite", "")
	if err := w.Start([]string{"id", "note"}); err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{int64(1), "a,b"},
		{int64(2), `say "hi"`},
		{int64(3), "line1\nline2"},
		{int64(4), nil},
		{int64(5), []byte("中文")},
		{int64(6), []byte{0x00, 0xff}},
	}
	for _, r := range rows {
		if err := w.Row(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv must round-trip: %v\n%s", err, buf.String())
	}
	if len(recs) != 7 {
		t.Fatalf("records: %d", len(recs))
	}
	if recs[1][1] != "a,b" || recs[2][1] != `say "hi"` || recs[3][1] != "line1\nline2" {
		t.Fatalf("quoting: %#v", recs)
	}
	if recs[4][1] != "NULL" || recs[5][1] != "中文" {
		t.Fatalf("values: %#v", recs[4:6])
	}
	if !strings.HasPrefix(recs[6][1], "base64:") {
		t.Fatalf("binary must be base64: %q", recs[6][1])
	}
}

func TestExportJSONTypes(t *testing.T) {
	var buf bytes.Buffer
	w := NewExportWriter(&buf, "json", "sqlite", "")
	if err := w.Start([]string{"id", "name", "ok", "n"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Row([]any{int64(1), "alice", true, nil}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, buf.String())
	}
	if got[0]["id"] != float64(1) || got[0]["name"] != "alice" || got[0]["ok"] != true {
		t.Fatalf("row: %#v", got[0])
	}
	if v, ok := got[0]["n"]; !ok || v != nil {
		t.Fatalf("null: %#v", got[0])
	}
}

func TestExportInsertRows(t *testing.T) {
	var buf bytes.Buffer
	w := NewExportWriter(&buf, "insert", "mysql", "users")
	if err := w.Start([]string{"id", "name"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Row([]any{int64(1), "O'Reilly"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `users` (`id`, `name`) VALUES (1, 'O''Reilly');"
	if strings.TrimSpace(buf.String()) != want {
		t.Fatalf("insert:\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestExportStreamWritesAllRows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.csv"
	n, err := ExportStream(path, "csv", "sqlite", "", func(onCols func([]string) error, onRow func([]any) error) error {
		if err := onCols([]string{"id"}); err != nil {
			return err
		}
		for i := 0; i < 3; i++ {
			if err := onRow([]any{i}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("stream: n=%d err=%v", n, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "id\n0\n1\n2") {
		t.Fatalf("csv content: %q", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perms: %v", info.Mode().Perm())
	}
}

func TestSQLStringEscapingKeepsJSON(t *testing.T) {
	jsonText := `{"name":"O'Reilly","path":"C:\\tmp","quote":"say \"hi\""}`
	if got, want := QuoteValueFor("mysql", jsonText),
		`'{"name":"O''Reilly","path":"C:\\\\tmp","quote":"say \\"hi\\""}'`; got != want {
		t.Fatalf("mysql literal:\n got %s\nwant %s", got, want)
	}
	if got, want := QuoteValueFor("sqlite", jsonText),
		`'{"name":"O''Reilly","path":"C:\\tmp","quote":"say \"hi\""}'`; got != want {
		t.Fatalf("sqlite literal:\n got %s\nwant %s", got, want)
	}
	row := []any{int64(1), jsonText}
	ins := InsertStatement("mysql", "meeting_case3", []string{"id", "payload"}, row)
	if !strings.Contains(ins, `C:\\\\tmp`) || !strings.Contains(ins, `say \\"hi\\"`) {
		t.Fatalf("insert must use mysql escaping: %s", ins)
	}
}

func TestMySQLDateTimeText(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2030, 11, 11, 18, 28, 47, 0, zone), "2030-11-11 18:28:47"},
		{time.Date(2030, 11, 11, 18, 28, 47, 123456000, zone), "2030-11-11 18:28:47.123456"},
		{time.Date(2030, 11, 11, 18, 28, 47, 500000000, zone), "2030-11-11 18:28:47.5"},
		{time.Time{}, "0000-00-00 00:00:00"},
	}
	for _, c := range cases {
		if got := MySQLDateTimeText(c.in); got != c.want {
			t.Fatalf("MySQLDateTimeText(%v) = %q want %q", c.in, got, c.want)
		}
	}
	for _, typ := range []string{"DATETIME", "timestamp", " DateTime "} {
		if !MySQLDateTimeType(typ) {
			t.Fatalf("%q 应当识别为 MySQL 日期时间", typ)
		}
	}
	for _, typ := range []string{"DATE", "VARCHAR", "", "TIMESTAMP(6)"} {
		if MySQLDateTimeType(typ) {
			t.Fatalf("%q 不应识别为 MySQL DATETIME/TIMESTAMP", typ)
		}
	}
}

func TestInsertStatementWithColumnTypes(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	ts := time.Date(2030, 11, 11, 18, 28, 47, 0, zone)
	cols := []string{"id", "created_at", "updated_at"}
	types := []string{"BIGINT", "DATETIME", "TIMESTAMP"}
	row := []any{int64(1), ts, ts}
	got := InsertStatementFor("mysql", "t", cols, types, row)
	want := "INSERT INTO `t` (`id`, `created_at`, `updated_at`) VALUES (1, '2030-11-11 18:28:47', '2030-11-11 18:28:47');"
	if got != want {
		t.Fatalf("typed insert:\n got %q\nwant %q", got, want)
	}
	// DATE 不走特殊格式，保持原有 RFC3339 行为。
	got = InsertStatementFor("mysql", "t", []string{"d"}, []string{"DATE"}, []any{ts})
	if !strings.Contains(got, "'2030-11-11T18:28:47+08:00'") {
		t.Fatalf("DATE must keep RFC3339: %s", got)
	}
	// 非 MySQL 数据源不特殊处理。
	got = InsertStatementFor("postgres", "t", []string{"ts"}, []string{"TIMESTAMP"}, []any{ts})
	if !strings.Contains(got, "'2030-11-11T18:28:47+08:00'") {
		t.Fatalf("postgres must keep RFC3339: %s", got)
	}
}
