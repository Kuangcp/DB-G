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

func (f statsFakeSession) ObjectStatsIn(context.Context, string, engine.ObjectKind, string) (*engine.ObjectStats, error) {
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
				"SELECT TABLE_ROWS FROM information_schema.TABLES",
			},
			ExactRowsSQL: "SELECT COUNT(*) FROM users",
		},
	})
	a.tree.Expand[connID] = true
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
	if !a.modal.StatsOpen || a.modal.StatsLoading || a.modal.StatsErr != "" {
		t.Fatalf("stats modal: open=%v loading=%v err=%q", a.modal.StatsOpen, a.modal.StatsLoading, a.modal.StatsErr)
	}
	for _, want := range []string{"统计：users", "3", "2", "2.0 KiB", "1.0 KiB", "InnoDB"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %q", want, tt.Texts())
		}
	}
	// 统计 SQL 默认折叠，但复制按钮不受折叠影响。
	if tt.HasText("SELECT COUNT(*) FROM users") || tt.HasText("精确行数（未执行，大表可能很慢）") {
		t.Fatalf("statistics SQL must start collapsed: %q", tt.Texts())
	}
	if err := tt.Click("复制 SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, "SELECT COUNT(*) FROM users;") ||
		!strings.Contains(got, "information_schema.TABLES;") ||
		!strings.Contains(got, "精确行数（未执行，大表可能很慢）") {
		t.Fatalf("clipboard = %q", got)
	}
	if err := tt.Click("统计 SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.modal.StatsSQLOpen || !strings.Contains(a.statsSQLView().Buffer.String(), "SELECT COUNT(*) FROM users") || !strings.Contains(a.statsSQLView().Buffer.String(), "精确行数") {
		t.Fatalf("expanded statistics SQL missing: open=%v text=%q", a.modal.StatsSQLOpen, a.statsSQLView().Buffer.String())
	}
	// 关闭后不再渲染。
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.modal.StatsOpen {
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
	a.tree.Expand[connID] = true
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
	if !strings.Contains(a.modal.StatsErr, "统计查询炸了") {
		t.Fatalf("stats error = %q", a.modal.StatsErr)
	}
}

func TestObjectStatsOnlyForTables(t *testing.T) {
	a, connID := seedStatsRoot(t)
	rows, columns := int64(3), int64(2)
	a.testSetSession(connID, statsFakeSession{
		treeFakeSession: treeFakeSession{},
		stats:           &engine.ObjectStats{Rows: &rows, Columns: &columns},
	})
	a.tree.Data[connID] = map[engine.ObjectKind][]engine.DBObject{
		engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "users"}},
		engine.ObjectView:  {{Kind: engine.ObjectView, Name: "user_view"}},
	}
	a.tree.Counts[connID] = map[engine.ObjectKind]int{
		engine.ObjectTable: 1, engine.ObjectView: 1,
	}
	a.tree.Expand[connID] = true
	*a.groupOpenPtr(connID+"|"+kindKey(engine.ObjectView), false) = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("user_view"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("查看统计"); err == nil {
		t.Fatal("views must not offer statistics")
	}
	p, _ := a.store.Get(connID)
	a.showStats(p, engine.DBObject{Kind: engine.ObjectView, Name: "user_view"})
	if a.modal.StatsOpen {
		t.Fatal("showStats must reject views")
	}
}
