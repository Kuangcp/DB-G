package ui

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// pagedFakeSession 模拟有几万张表的大库：只实现计数 + 分页，全量 Objects 会报错，
// 用来确认树不会走全量加载。
type pagedFakeSession struct {
	total       int
	pageCalls   int
	countCalls  int
	objectsCall int
}

func (f *pagedFakeSession) Open(context.Context) error { return nil }
func (f *pagedFakeSession) Close() error               { return nil }
func (f *pagedFakeSession) Cancel() bool               { return false }
func (f *pagedFakeSession) Caps() engine.Capabilities  { return engine.CapObjects }
func (f *pagedFakeSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (f *pagedFakeSession) Objects(context.Context, engine.ObjectKind) ([]engine.DBObject, error) {
	f.objectsCall++
	return nil, fmt.Errorf("full Objects must not be called")
}
func (f *pagedFakeSession) ObjectCounts(context.Context, string) (map[engine.ObjectKind]int, error) {
	f.countCalls++
	return map[engine.ObjectKind]int{engine.ObjectTable: f.total, engine.ObjectView: 0, engine.ObjectIndex: 0}, nil
}
func (f *pagedFakeSession) ObjectsPage(_ context.Context, _ string, kind engine.ObjectKind, offset, limit int) ([]engine.DBObject, error) {
	f.pageCalls++
	if kind != engine.ObjectTable {
		return nil, nil
	}
	if offset >= f.total {
		return nil, nil
	}
	end := offset + limit
	if limit <= 0 || end > f.total {
		end = f.total
	}
	out := make([]engine.DBObject, 0, end-offset)
	for i := offset; i < end; i++ {
		out = append(out, engine.DBObject{Kind: kind, Name: fmt.Sprintf("table_%05d", i)})
	}
	return out, nil
}

func TestObjectTreeLargeSchemaUsesCountsAndPages(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "big", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	sess := &pagedFakeSession{total: 60000}
	a.testSetSession(stored.ID, sess)
	a.treeExpand[stored.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if sess.countCalls != 1 || sess.objectsCall != 0 || sess.pageCalls != 0 {
		t.Fatalf("connect must load counts only: counts=%d pages=%d full=%d", sess.countCalls, sess.pageCalls, sess.objectsCall)
	}
	if !tt.HasText("表 (60000)") {
		t.Fatalf("count label missing: %q", tt.Texts())
	}
	if len(a.treeData[stored.ID][engine.ObjectTable]) != 0 {
		t.Fatal("objects must load only when the group opens")
	}

	// 展开表分组：只拉第一页。
	*a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), false) = true
	tt.Frame()
	objs := a.treeData[stored.ID][engine.ObjectTable]
	if len(objs) != objectPageSize {
		t.Fatalf("first page = %d, want %d", len(objs), objectPageSize)
	}
	if !tt.HasText("table_00000") {
		t.Fatalf("first page row missing: %q", tt.Texts())
	}
	if tt.HasText("table_01000") {
		t.Fatalf("second page must not load yet: %q", tt.Texts())
	}
	label := fmt.Sprintf("继续加载（已 %d / 共 60000）", objectPageSize)
	if _, ok := tt.Find(label); !ok {
		t.Fatalf("load-more row missing: %q", tt.Texts())
	}
	// 行在树底部、窗口外，直接走同一入口验证追加（点击路径由 Tester 的命中检查限制）。
	a.loadObjectPage(stored, "", engine.ObjectTable, false)
	tt.Frame()
	if got := len(a.treeData[stored.ID][engine.ObjectTable]); got != 2*objectPageSize {
		t.Fatalf("second page = %d, want %d", got, 2*objectPageSize)
	}
	if sess.objectsCall != 0 {
		t.Fatal("large schema must never call full Objects")
	}
}

func TestObjectsOfCapsCatalog(t *testing.T) {
	sess := &pagedFakeSession{total: 60000}
	tables, views, err := objectsOf(sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != catalogObjectLimit || len(views) != 0 {
		t.Fatalf("catalog = %d tables / %d views", len(tables), len(views))
	}
	if sess.objectsCall != 0 {
		t.Fatal("paged catalog must not fall back to full Objects")
	}
}
