package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestPageSQL(t *testing.T) {
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
		got, ok := pageSQL(c.sql, 500, 100)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("pageSQL(%q) = %q,%v want %q,%v", c.sql, got, ok, c.want, c.ok)
		}
	}
	if got, ok := pageCountSQL("clickhouse", "SELECT 1"); !ok || got != "SELECT count() FROM (SELECT 1) AS dbg_count" {
		t.Fatalf("clickhouse count: %q %v", got, ok)
	}
	if got, ok := pageCountSQL("sqlite", "SELECT 1;"); !ok || got != "SELECT COUNT(*) FROM (SELECT 1) AS dbg_count" {
		t.Fatalf("sqlite count: %q %v", got, ok)
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

func TestPageNavigationAndCount(t *testing.T) {
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
	ps := a.tabs.PageStates[a.currentRuntimeID()]
	if got := len(a.con.Result().Rows); got != 500 || ps == nil || !ps.HasMore {
		t.Fatalf("first page rows=%d state=%+v", got, ps)
	}
	tt.Frame()
	for _, want := range []string{"第 1 页", "每页 500 行"} {
		if !tt.HasText(want) {
			t.Fatalf("pager missing %q: %q", want, tt.Texts())
		}
	}
	if _, ok := tt.Find("统计总行数"); !ok {
		t.Fatalf("count button missing: %q", tt.Texts())
	}
	if !a.canNextPage(ps) {
		t.Fatal("next page must be available")
	}
	a.changePage(1)
	waitPage(t, a, tt, func() bool {
		return a.tabs.PageStates[a.currentRuntimeID()].Page == 1 && len(a.con.Result().Rows) == 100
	})
	ps = a.tabs.PageStates[a.currentRuntimeID()]
	if ps.TotalKnown || ps.HasMore {
		t.Fatalf("last page state: %+v", ps)
	}
	if a.canNextPage(ps) {
		t.Fatal("next page must be disabled on the last page")
	}
	a.changePage(-1)
	waitPage(t, a, tt, func() bool {
		return a.tabs.PageStates[a.currentRuntimeID()].Page == 0 && len(a.con.Result().Rows) == 500
	})
	a.countRows()
	waitPage(t, a, tt, func() bool {
		ps := a.tabs.PageStates[a.currentRuntimeID()]
		return ps != nil && ps.TotalKnown && ps.Total == 600
	})
	tt.Frame()
	if !tt.HasText("共 600 行") || !tt.HasText("/ 2 页") {
		t.Fatalf("total count missing: %q", tt.Texts())
	}
}

func waitPage(t *testing.T, a *root, tt *ui.Tester, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("page operation did not finish")
}

func TestDisplayToResultAfterSort(t *testing.T) {
	a := newTestRoot()
	r := resultWith([]string{"id"},
		[]any{int64(30)}, []any{int64(10)}, []any{int64(20)})
	a.grid.view.sortBy(r, 0, false)
	got := a.grid.view.apply(r)
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
	progress := 0
	n, err = a.exportToProgress(jsonPath, "json", true, func(v int) { progress = v }) // 全量流式
	if err != nil || n != 2 || progress != 2 {
		t.Fatalf("full export: n=%d progress=%d err=%v", n, progress, err)
	}
	data, _ = os.ReadFile(jsonPath)
	if !strings.Contains(string(data), `"name":"bob"`) {
		t.Fatalf("json: %q", data)
	}

	jsonlPath := dir + "/out.jsonl"
	if _, err := a.exportTo(jsonlPath, "jsonl", false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(jsonlPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "{") || !strings.Contains(lines[1], `"name":"bob"`) {
		t.Fatalf("current jsonl: %q", data)
	}

	fullJSONL := dir + "/all.jsonl"
	progress = 0
	n, err = a.exportToProgress(fullJSONL, "jsonl", true, func(v int) { progress = v })
	if err != nil || n != 2 || progress != 2 {
		t.Fatalf("full jsonl: n=%d progress=%d err=%v", n, progress, err)
	}
	data, _ = os.ReadFile(fullJSONL)
	if len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 2 {
		t.Fatalf("full jsonl lines: %q", data)
	}
}

func TestExportStatusDoubleClickOpensFolder(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{IsQuery: true, Columns: []string{"id"}, Rows: [][]any{{int64(1)}}}, "")
	a.exportState.Status = "Exported 2 rows → /tmp/out.csv"
	a.exportState.Path = "/tmp/out.csv"
	var got string
	a.revealPath = func(path string) error {
		got = path
		return nil
	}
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if err := tt.Click("Exported 2 rows → /tmp/out.csv"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Exported 2 rows → /tmp/out.csv"); err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/out.csv" {
		t.Fatalf("reveal path = %q", got)
	}
}
