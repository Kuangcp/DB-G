package ui

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
)

// schemaFakeSession 模拟多 schema 数据源：记录 UseSchema 与 DDL 用的 schema，
// 按 schema 返回不同的对象/列，用来确认 UI 不会把不同库的元数据混在一起。
type schemaFakeSession struct {
	treeFakeSession
	schemas   []string
	active    string
	used      []string
	objects   map[string][]engine.DBObject
	columns   map[string]map[string][]engine.Column
	ddlSchema string
}

func (f *schemaFakeSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapObjectDDL
}

func (f *schemaFakeSession) Schemas(context.Context) ([]string, error) { return f.schemas, nil }

func (f *schemaFakeSession) ObjectsPage(_ context.Context, schema string, kind engine.ObjectKind, _, _ int) ([]engine.DBObject, error) {
	var out []engine.DBObject
	for _, o := range f.objects[schema] {
		if o.Kind == kind {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *schemaFakeSession) ObjectsIn(ctx context.Context, schema string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	return f.ObjectsPage(ctx, schema, kind, 0, 0)
}

func (f *schemaFakeSession) UseSchema(_ context.Context, schema string) error {
	f.active = schema
	f.used = append(f.used, schema)
	return nil
}

func (f *schemaFakeSession) ActiveSchema(context.Context) (string, error) { return f.active, nil }

func (f *schemaFakeSession) ColumnsIn(_ context.Context, schema, table string) ([]engine.Column, error) {
	return f.columns[schema][table], nil
}

func (f *schemaFakeSession) ObjectDDLIn(_ context.Context, schema string, _ engine.ObjectKind, _ string) (string, error) {
	f.ddlSchema = schema
	return "CREATE TABLE " + schema + ".t (id INT)", nil
}

func newSchemaTestRoot(t *testing.T, p app.ConnProfile) (*root, app.ConnProfile, *schemaFakeSession) {
	t.Helper()
	a := newSidebarRoot(t)
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	fake := &schemaFakeSession{
		schemas: []string{"db1", "db2"},
		objects: map[string][]engine.DBObject{
			"db1": {{Kind: engine.ObjectTable, Name: "only_db1", Schema: "db1"}},
			"db2": {{Kind: engine.ObjectTable, Name: "only_db2", Schema: "db2"}},
		},
		columns: map[string]map[string][]engine.Column{
			"db1": {"t": {{Name: "col_db1"}}},
			"db2": {"t": {{Name: "col_db2"}}},
		},
	}
	a.testSetSession(stored.ID, fake)
	a.treeSchemas[stored.ID] = fake.schemas
	return a, stored, fake
}

func TestDefaultSchemaForPrefersConfiguredDatabase(t *testing.T) {
	if got := defaultSchemaFor(app.ConnProfile{Kind: "mysql", Database: "app"}, []string{"a", "app"}); got != "app" {
		t.Fatalf("mysql default = %q", got)
	}
	if got := defaultSchemaFor(app.ConnProfile{Kind: "postgres", Database: "app"}, []string{"app", "public"}); got != "public" {
		t.Fatalf("postgres default = %q", got)
	}
	if got := defaultSchemaFor(app.ConnProfile{Kind: "clickhouse"}, []string{"analytics", "default"}); got != "analytics" {
		t.Fatalf("fallback default = %q", got)
	}
}

func TestTopBarSchemaPickerSwitchesSchema(t *testing.T) {
	a, stored, fake := newSchemaTestRoot(t, app.ConnProfile{Name: "多库", Kind: "mysql", Host: "h", Port: 3306})
	a.connSchema[stored.ID] = "db1"
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("db1") {
		t.Fatalf("当前库未显示: %q", tt.Texts())
	}
	tt.Click("db1")
	tt.Frame()
	tt.Click("db2")
	tt.Frame()
	if fake.active != "db2" {
		t.Fatalf("UseSchema = %q, want db2", fake.active)
	}
	if got := a.connSchema[stored.ID]; got != "db2" {
		t.Fatalf("connSchema = %q", got)
	}
	if a.treeOpen[stored.ID+"|db2|schema"] == nil || !*a.treeOpen[stored.ID+"|db2|schema"] {
		t.Fatal("切换后应展开对应 schema 节点")
	}
}

func TestSchemaSwitchScopesCompletionCache(t *testing.T) {
	a, stored, _ := newSchemaTestRoot(t, app.ConnProfile{Name: "多库", Kind: "mysql", Host: "h", Port: 3306})
	a.connSchema[stored.ID] = "db1"
	a.ensureCatalog(stored.ID)
	items := a.completionItems(stored.ID)
	if !hasCompletion(items, "only_db1") || hasCompletion(items, "only_db2") {
		t.Fatalf("db1 目录串库: %v", labelsOf(items))
	}
	a.ensureColumns(stored.ID, "t")
	key, _ := a.completionScope(stored.ID)
	cols, ok := a.catalog.getColumns(key, "t")
	if !ok || !hasCompletion(cols, "col_db1") {
		t.Fatalf("db1 列未缓存: %v", labelsOf(cols))
	}

	a.setActiveSchema(stored, "db2")
	a.ensureCatalog(stored.ID)
	items = a.completionItems(stored.ID)
	if !hasCompletion(items, "only_db2") || hasCompletion(items, "only_db1") {
		t.Fatalf("切库后目录未重置: %v", labelsOf(items))
	}
	a.ensureColumns(stored.ID, "t")
	db2Key, _ := a.completionScope(stored.ID)
	cols, ok = a.catalog.getColumns(db2Key, "t")
	if !ok || !hasCompletion(cols, "col_db2") || hasCompletion(cols, "col_db1") {
		t.Fatalf("切库后列缓存不对: ok=%v %v", ok, labelsOf(cols))
	}
}

func TestDDLUsesObjectSchema(t *testing.T) {
	a, stored, fake := newSchemaTestRoot(t, app.ConnProfile{Name: "多库", Kind: "mysql", Host: "h", Port: 3306})
	p, _ := a.store.Get(stored.ID)
	a.showDDL(p, engine.DBObject{Kind: engine.ObjectTable, Name: "t", Schema: "db2"})
	if fake.ddlSchema != "db2" {
		t.Fatalf("DDL schema = %q, want db2", fake.ddlSchema)
	}
}

func hasCompletion(items []myeditor.Completion, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

func labelsOf(items []myeditor.Completion) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}
