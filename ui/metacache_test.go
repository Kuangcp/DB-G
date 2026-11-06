package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

func cacheTestProfile(t *testing.T, a *root) app.ConnProfile {
	t.Helper()
	p := app.ConnProfile{Name: "cached", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	return a.store.List()[0]
}

func cachePayload(name string, n int) app.MetaCachePayload {
	return app.MetaCachePayload{
		Counts: map[string]map[engine.ObjectKind]int{
			"": {engine.ObjectTable: n},
		},
		Objects: map[string]map[engine.ObjectKind][]engine.DBObject{
			"": {
				engine.ObjectTable: {{Kind: engine.ObjectTable, Name: name}},
			},
		},
	}
}

func TestMetaCacheRendersTreeWithoutSession(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	if err := a.metaCache.Save(p, cachePayload("cached_table", 1)); err != nil {
		t.Fatal(err)
	}
	a.loadMetaCache(p)
	a.treeExpand[p.ID] = true
	*a.groupOpenPtr(p.ID+"|"+kindKey(engine.ObjectTable), false) = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("表 (1)") || !tt.HasText("cached_table") {
		t.Fatalf("cached tree missing: %q", tt.Texts())
	}
	if a.connected(p.ID) {
		t.Fatal("cache must render without opening a session")
	}
}

func TestMetaCacheStaleRefreshesCountsAndPage(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	a.metaCache = app.NewMetaCacheWithTTL(a.metaCache.MetaCachePath(p), 0) // 目录不对，下面重设
	// 用独立目录和 0 TTL，保证读到即过期。
	a.metaCache = app.NewMetaCacheWithTTL(t.TempDir(), 0)
	if err := a.metaCache.Save(p, cachePayload("cached_old", 1)); err != nil {
		t.Fatal(err)
	}
	sess := &pagedFakeSession{total: 2}
	a.testSetSession(p.ID, sess)
	a.loadMetaCache(p)
	a.treeExpand[p.ID] = true
	*a.groupOpenPtr(p.ID+"|"+kindKey(engine.ObjectTable), false) = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if tt.HasText("cached_old") {
		t.Fatalf("stale cached rows must be replaced: %q", tt.Texts())
	}
	if !tt.HasText("表 (2)") || !tt.HasText("table_00000") {
		t.Fatalf("refreshed tree missing: %q", tt.Texts())
	}
	if sess.objectsCall != 0 {
		t.Fatal("stale refresh must not fall back to full Objects")
	}
	got, hit, _ := a.metaCache.Load(p)
	if !hit || got.Counts[""][engine.ObjectTable] != 2 {
		t.Fatalf("refreshed cache: hit=%v %+v", hit, got.Counts)
	}
	if objs := got.Objects[""][engine.ObjectTable]; len(objs) != 2 {
		t.Fatalf("refreshed cache objects: %+v", objs)
	}
}

func TestReloadAndRemoveClearMetaCache(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	if err := a.metaCache.Save(p, cachePayload("t", 1)); err != nil {
		t.Fatal(err)
	}
	a.reloadTree()
	if _, hit, _ := a.metaCache.Load(p); hit {
		t.Fatal("reloadTree must clear disk cache")
	}
	if err := a.metaCache.Save(p, cachePayload("t", 1)); err != nil {
		t.Fatal(err)
	}
	a.removeConn(p.ID)
	if _, hit, _ := a.metaCache.Load(p); hit {
		t.Fatal("removing a connection must clear its cache")
	}
}

func TestMetaCacheRendersCachedSchemaTree(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	payload := app.MetaCachePayload{
		Schemas: []string{"db1"},
		Counts: map[string]map[engine.ObjectKind]int{
			"db1": {engine.ObjectTable: 1},
		},
		Objects: map[string]map[engine.ObjectKind][]engine.DBObject{
			"db1": {engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "cached_schema_table", Schema: "db1"}}},
		},
	}
	if err := a.metaCache.Save(p, payload); err != nil {
		t.Fatal(err)
	}
	a.loadMetaCache(p)
	a.treeExpand[p.ID] = true
	*a.groupOpenPtr(p.ID+"|db1|schema", false) = true
	*a.groupOpenPtr(p.ID+"|db1|"+kindKey(engine.ObjectTable), false) = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("▣ db1") || !tt.HasText("表 (1)") || !tt.HasText("cached_schema_table") {
		t.Fatalf("cached schema tree missing: %q", tt.Texts())
	}
}

func TestMetaCacheSavedAfterMetadataLoad(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	a.testSetSession(p.ID, &pagedFakeSession{total: 3})
	a.loadGroupsIn(p, "")
	got, hit, _ := a.metaCache.Load(p)
	if !hit {
		t.Fatal("metadata load must write disk cache")
	}
	if got.Counts[""][engine.ObjectTable] != 3 {
		t.Fatalf("counts not cached: %+v", got.Counts)
	}
	if objs := got.Objects[""][engine.ObjectTable]; len(objs) != 3 {
		t.Fatalf("objects not cached: %+v", objs)
	}
}

func TestMetaCacheFreshHitAvoidsQueries(t *testing.T) {
	a := newSidebarRoot(t)
	p := cacheTestProfile(t, a)
	if err := a.metaCache.Save(p, cachePayload("cached_table", 1)); err != nil {
		t.Fatal(err)
	}
	sess := &pagedFakeSession{total: 1}
	a.testSetSession(p.ID, sess)
	a.loadMetaCache(p)
	a.treeExpand[p.ID] = true
	*a.groupOpenPtr(p.ID+"|"+kindKey(engine.ObjectTable), false) = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("cached_table") {
		t.Fatalf("cached tree missing: %q", tt.Texts())
	}
	if sess.countCalls != 0 || sess.pageCalls != 0 || sess.objectsCall != 0 {
		t.Fatalf("fresh cache must not query: counts=%d pages=%d full=%d",
			sess.countCalls, sess.pageCalls, sess.objectsCall)
	}
}
