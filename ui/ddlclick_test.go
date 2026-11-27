package ui

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

type ddlClickSession struct {
	treeFakeSession
	calls int
}

func (f *ddlClickSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapObjectDDL
}

func (f *ddlClickSession) ObjectDDL(context.Context, engine.ObjectKind, string) (string, error) {
	f.calls++
	return "CREATE TABLE users (id INTEGER PRIMARY KEY);", nil
}

func newDDLClickRoot(t *testing.T) (*root, *ddlClickSession, *ui.Tester) {
	t.Helper()
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	fake := &ddlClickSession{treeFakeSession: treeFakeSession{objs: []engine.DBObject{
		{Kind: engine.ObjectTable, Name: "users"},
	}}}
	a.testSetSession(stored.ID, fake)
	a.tree.Expand[stored.ID] = true
	a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	return a, fake, tt
}

func TestCtrlClickObjectOpensDDL(t *testing.T) {
	a, fake, tt := newDDLClickRoot(t)
	rect, ok := tt.Find("users")
	if !ok {
		t.Fatalf("对象行不存在: %v", tt.Texts())
	}
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	if !a.modal.DDLOpen {
		t.Fatalf("Ctrl+点击应打开 DDL: calls=%d", fake.calls)
	}
	if fake.calls != 1 {
		t.Fatalf("应只请求一次 DDL，得到 %d", fake.calls)
	}
}

func TestPlainClickObjectDoesNotOpenDDL(t *testing.T) {
	a, fake, tt := newDDLClickRoot(t)
	rect, ok := tt.Find("users")
	if !ok {
		t.Fatalf("对象行不存在: %v", tt.Texts())
	}
	tt.ClickAt(rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	if a.modal.DDLOpen || fake.calls != 0 {
		t.Fatalf("普通点击不应打开 DDL: open=%v calls=%d", a.modal.DDLOpen, fake.calls)
	}
}

func TestCtrlDoubleClickObjectAtMostOneDDL(t *testing.T) {
	_, fake, tt := newDDLClickRoot(t)
	rect, ok := tt.Find("users")
	if !ok {
		t.Fatalf("对象行不存在: %v", tt.Texts())
	}
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+rect.H/2)
	tt.ClickAtWith(ui.Ctrl, rect.X+2, rect.Y+rect.H/2)
	tt.Frame()
	if fake.calls > 1 {
		t.Fatalf("Ctrl+双击最多请求一次 DDL，得到 %d", fake.calls)
	}
}
