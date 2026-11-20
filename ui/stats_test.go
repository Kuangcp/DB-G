package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

type statsFakeSession struct {
	treeFakeSession
	stats *engine.ObjectStats
	err   error
}

func (f statsFakeSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapObjectStats
}

func (f statsFakeSession) ObjectStats(context.Context, engine.ObjectKind, string) (*engine.ObjectStats, error) {
	return f.stats, f.err
}

func seedStatsRoot(t *testing.T) (*root, string) {
	t.Helper()
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	return a, stored.ID
}

func TestObjectStatsModal(t *testing.T) {
	a, connID := seedStatsRoot(t)
	rows, columns, data, index := int64(3), int64(2), int64(2048), int64(1024)
	a.testSetSession(connID, statsFakeSession{
		treeFakeSession: treeFakeSession{objs: []engine.DBObject{
			{Kind: engine.ObjectTable, Name: "users"},
		}},
		stats: &engine.ObjectStats{
			Rows: &rows, Columns: &columns, DataBytes: &data, IndexBytes: &index,
			Extra: []engine.ObjectStat{{Key: "statMySQLEngine", Value: "InnoDB"}},
			SQL: []string{
				"SELECT COUNT(*) FROM users",
				"SELECT DATA_LENGTH, INDEX_LENGTH FROM information_schema.TABLES",
			},
		},
	})
	a.treeExpand[connID] = true
	a.groupOpenPtr(connID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("users"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("查看统计"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.statsOpen || a.statsLoading || a.statsErr != "" {
		t.Fatalf("stats modal: open=%v loading=%v err=%q", a.statsOpen, a.statsLoading, a.statsErr)
	}
	for _, want := range []string{"统计：users", "3", "2", "2.0 KiB", "1.0 KiB", "InnoDB"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %q", want, tt.Texts())
		}
	}
	// 统计 SQL 默认折叠，但复制按钮不受折叠影响。
	if tt.HasText("SELECT COUNT(*) FROM users") {
		t.Fatalf("statistics SQL must start collapsed: %q", tt.Texts())
	}
	if err := tt.Click("复制 SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, "SELECT COUNT(*) FROM users;") ||
		!strings.Contains(got, "information_schema.TABLES;") {
		t.Fatalf("clipboard = %q", got)
	}
	if err := tt.Click("统计 SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.statsSQLOpen || !strings.Contains(a.statsSQLView().Buffer.String(), "SELECT COUNT(*) FROM users") {
		t.Fatalf("expanded statistics SQL missing: open=%v text=%q", a.statsSQLOpen, a.statsSQLView().Buffer.String())
	}
	// 关闭后不再渲染。
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.statsOpen {
		t.Fatal("stats modal must close")
	}
}

func TestObjectStatsUnsupported(t *testing.T) {
	a, connID := seedStatsRoot(t)
	a.testSetSession(connID, statsFakeSession{
		treeFakeSession: treeFakeSession{objs: []engine.DBObject{
			{Kind: engine.ObjectTable, Name: "users"},
		}},
		err: errors.New("统计查询炸了"),
	})
	a.treeExpand[connID] = true
	a.groupOpenPtr(connID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("users"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("查看统计"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(a.statsErr, "统计查询炸了") {
		t.Fatalf("stats error = %q", a.statsErr)
	}
}
