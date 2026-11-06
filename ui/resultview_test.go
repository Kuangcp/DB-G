package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestFetchMoreSQL(t *testing.T) {
	cases := []struct {
		sql  string
		ok   bool
		want string
	}{
		{"SELECT * FROM t", true, "SELECT * FROM (SELECT * FROM t) AS dbg LIMIT 100 OFFSET 500"},
		{"SELECT * FROM t;", true, "SELECT * FROM (SELECT * FROM t) AS dbg LIMIT 100 OFFSET 500"},
		{"  select id from t  ;  ", true, "SELECT * FROM (select id from t) AS dbg LIMIT 100 OFFSET 500"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", true, "SELECT * FROM (WITH x AS (SELECT 1) SELECT * FROM x) AS dbg LIMIT 100 OFFSET 500"},
		{"SELECT 1; SELECT 2", false, ""},
		{"UPDATE t SET a = 1", false, ""},
		{"", false, ""},
	}
	for _, c := range cases {
		got, ok := fetchMoreSQL(c.sql, 500, 100)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("fetchMoreSQL(%q) = %q,%v want %q,%v", c.sql, got, ok, c.want, c.ok)
		}
	}
}

func TestResultViewSortAndFilter(t *testing.T) {
	r := resultWith([]string{"id", "name"},
		[]any{int64(3), "carol"},
		[]any{int64(1), "alice"},
		[]any{nil, "Bob"},
		[]any{int64(2), "bob"},
	)
	v := newResultView()
	v.sortBy(r, 0, false) // 数字升序，NULL 最后
	got := v.apply(r)
	if got[0] != 1 || got[1] != 3 || got[2] != 0 || got[3] != 2 {
		t.Fatalf("asc order: %v", got)
	}
	v.sortBy(r, 0, true)
	got = v.apply(r)
	if got[0] != 0 || got[3] != 2 {
		t.Fatalf("desc order: %v", got)
	}
	// 字符串过滤（大小写不敏感）
	v = newResultView()
	v.setFilter(1, "BO")
	got = v.apply(r)
	if len(got) != 2 {
		t.Fatalf("filter: %v", got)
	}
	v.clearFilter(1)
	if len(v.apply(r)) != 4 {
		t.Fatal("clear filter must restore all rows")
	}
}

func TestCompareCells(t *testing.T) {
	if compareCells(nil, int64(1)) <= 0 {
		t.Fatal("nil must sort last")
	}
	if compareCells(int64(2), int64(10)) >= 0 {
		t.Fatal("numbers compared by value")
	}
	if compareCells("a", "b") >= 0 {
		t.Fatal("strings compared lexicographically")
	}
	if compareCells([]byte("x"), "y") >= 0 {
		t.Fatal("[]byte compares as text")
	}
}

func TestFetchMoreAppendsRows(t *testing.T) {
	a := openForSelection(t)
	if _, err := a.testSession(t).RunStatement(t.Context(), `CREATE TABLE big (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 600; i++ {
		if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO big (id) VALUES ("+strconv.Itoa(i)+")"); err != nil {
			t.Fatal(err)
		}
	}
	a.setActiveText("SELECT id FROM big ORDER BY id")
	a.setSelection(0, len([]rune("SELECT id FROM big ORDER BY id")))
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	tt.Frame()
	if got := len(a.con.Result().Rows); got != 500 {
		t.Fatalf("first page rows = %d", got)
	}
	if !a.canFetchMore() {
		t.Fatal("400 more rows must be fetchable")
	}
	a.fetchMore()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if len(a.con.Result().Rows) >= 600 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(a.con.Result().Rows); got != 600 {
		t.Fatalf("after fetch more rows = %d", got)
	}
	if a.canFetchMore() {
		t.Fatal("no more rows expected")
	}
}

func TestDisplayToResultAfterSort(t *testing.T) {
	a := newTestRoot()
	r := resultWith([]string{"id"},
		[]any{int64(30)}, []any{int64(10)}, []any{int64(20)})
	a.resView.sortBy(r, 0, false)
	got := a.resView.apply(r)
	if got[0] != 1 || got[1] != 2 || got[2] != 0 {
		t.Fatalf("sort order: %v", got)
	}
	if a.displayToResult(r, 0) != 1 || a.displayToResult(r, 2) != 0 {
		t.Fatalf("display→result mapping: %d %d", a.displayToResult(r, 0), a.displayToResult(r, 2))
	}
}

func TestExportCurrentAndFull(t *testing.T) {
	a := openForSelection(t)
	if _, err := a.testSession(t).RunStatement(t.Context(), "INSERT INTO users (name) VALUES ('alice'),('bob')"); err != nil {
		t.Fatal(err)
	}
	a.setActiveText("SELECT id, name FROM users ORDER BY id")
	a.setSelection(0, len([]rune("SELECT id, name FROM users ORDER BY id")))
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	a.runSQL()
	pollResult(t, a, tt)
	tt.Frame()

	dir := t.TempDir()
	csvPath := dir + "/out.csv"
	n, err := a.exportTo(csvPath, "csv", false)
	if err != nil || n != 2 {
		t.Fatalf("current export: n=%d err=%v", n, err)
	}
	data, _ := os.ReadFile(csvPath)
	if !strings.Contains(string(data), "id,name") || !strings.Contains(string(data), "alice") {
		t.Fatalf("csv: %q", data)
	}

	insPath := dir + "/out.sql"
	if _, err := a.exportTo(insPath, "insert", false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(insPath)
	if !strings.Contains(string(data), `INSERT INTO "users"`) {
		t.Fatalf("insert: %q", data)
	}

	jsonPath := dir + "/out.json"
	n, err = a.exportTo(jsonPath, "json", true) // 全量流式
	if err != nil || n != 2 {
		t.Fatalf("full export: n=%d err=%v", n, err)
	}
	data, _ = os.ReadFile(jsonPath)
	if !strings.Contains(string(data), `"name":"bob"`) {
		t.Fatalf("json: %q", data)
	}
}
