package ui

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"

	_ "db-g/driver/sqlite"
)

func newSidebarRoot(t *testing.T) *root {
	t.Helper()
	a := newTestRoot()
	a.store = app.LoadStore(filepath.Join(t.TempDir(), "connections.json"))
	a.metaCache = app.NewMetaCache(filepath.Join(t.TempDir(), "meta"))
	return a
}

func seedConnFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "seed", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	return path
}

func TestSidebarEmptyHint(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("新建连接") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestSidebarCreateConnFlow(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connFormOpen {
		t.Fatal("modal must open")
	}
	a.connFormName = "本地库"
	a.connFormPath = path
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connFormOpen {
		t.Fatal("modal must close after save")
	}
	if len(a.store.List()) != 1 || a.store.List()[0].Name != "本地库" {
		t.Fatalf("store: %+v", a.store.List())
	}
	tt.Frame()
	if !tt.HasText("本地库") {
		t.Fatalf("conn row not rendered: %q", tt.Texts())
	}
}

func TestSidebarCreateConnValidation(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.connFormName = "无路径"
	a.connFormPath = filepath.Join(t.TempDir(), "missing.db")
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if !a.connFormOpen || a.connFormErr == "" {
		t.Fatalf("invalid form must stay open with error: open=%v err=%q", a.connFormOpen, a.connFormErr)
	}
	if len(a.store.List()) != 0 {
		t.Fatal("invalid form must not persist")
	}
}

func TestOpenConnAndRunEndToEnd(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "本地库", Kind: "sqlite", Path: path}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	if !a.connected(stored.ID) {
		t.Fatal("openConn must install a session")
	}
	// 没有控制台时顶栏退回「最近用过的」那个数据源
	if got := a.activeConnName(); got != "本地库" || a.connErr != "" {
		t.Fatalf("activeConnName=%q connErr=%q", got, a.connErr)
	}
	if act, ok := a.store.Active(); !ok || act.ID != stored.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	a.setActiveText("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.setSelection(0, len([]rune("SELECT name FROM users")))
	a.runSQL()
	for range 200 {
		if a.con.Result() != nil || a.con.Err() != "" {
			break
		}
		tt.Frame()
	}
	if a.con.Err() != "" {
		t.Fatalf("exec err: %s", a.con.Err())
	}
	if a.con.Result() == nil {
		t.Fatal("expected result")
	}
}

func TestOpenConnFailureKeepsState(t *testing.T) {
	a := newSidebarRoot(t)
	if err := a.store.Add(app.ConnProfile{Name: "good", Kind: "sqlite", Path: seedConnFile(t)}); err != nil {
		t.Fatal(err)
	}
	good := a.store.List()[0]
	if err := a.openConn(good); err != nil {
		t.Fatal(err)
	}
	bad := app.ConnProfile{Name: "bad", Kind: "sqlite", Path: filepath.Join(t.TempDir(), "gone.db")}
	if err := a.openConn(bad); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErr == "" {
		t.Fatal("connErr must be set")
	}
	if len(a.sessions) != 1 || !a.connected(good.ID) {
		t.Fatalf("failed open must keep previous session: %d", len(a.sessions))
	}
	if got := a.connErrOf(bad.ID); got == "" {
		t.Fatal("失败原因要记在该数据源名下（圆点变红）")
	}
}

func TestRemoveActiveConnClearsSession(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "临时", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	if err := a.openConn(stored); err != nil {
		t.Fatal(err)
	}
	a.removeConn(stored.ID)
	if len(a.sessions) != 0 || a.connErr != "" {
		t.Fatalf("sessions=%d connErr=%q", len(a.sessions), a.connErr)
	}
	if _, ok := a.store.Get(stored.ID); ok {
		t.Fatal("profile must be removed")
	}
}

type treeFakeSession struct {
	err  error
	objs []engine.DBObject
}

func (f treeFakeSession) Open(context.Context) error { return nil }
func (f treeFakeSession) Close() error               { return nil }
func (f treeFakeSession) Cancel() bool               { return false }
func (f treeFakeSession) Caps() engine.Capabilities  { return engine.CapObjects }
func (f treeFakeSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (f treeFakeSession) Objects(context.Context, engine.ObjectKind) ([]engine.DBObject, error) {
	return f.objs, f.err
}

func TestObjectTreeRendersLeaves(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.testSetSession(stored.ID, treeFakeSession{objs: []engine.DBObject{
		{Kind: engine.ObjectTable, Name: "users"},
	}})
	a.treeExpand[stored.ID] = true
	a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("users") {
		t.Fatalf("leaf missing: %q", tt.Texts())
	}
	if !tt.HasText("表 (1)") {
		t.Fatalf("group count missing: %q", tt.Texts())
	}
}

func TestObjectTreeErrorLine(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.testSetSession(stored.ID, treeFakeSession{err: errors.New("枚举失败")})
	a.treeExpand[stored.ID] = true
	a.groupOpenPtr(stored.ID+"|"+kindKey(engine.ObjectTable), true)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("枚举失败") {
		t.Fatalf("error line missing: %q", tt.Texts())
	}
}

func TestReloadTreeClearsCache(t *testing.T) {
	a := newSidebarRoot(t)
	a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{"x": {}}
	a.treeErr = map[string]string{"x|table": "boom"}
	a.reloadTree()
	if a.treeData != nil || len(a.treeErr) != 0 {
		t.Fatalf("reloadTree must clear cache: %v %v", a.treeData, a.treeErr)
	}
}

func TestExpandInactiveAutoConnects(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "auto", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.treeExpand[stored.ID] = true
	a.expandConn(stored)
	if !a.connected(stored.ID) {
		t.Fatalf("expand of inactive conn must connect: %d sessions", len(a.sessions))
	}
}

func TestObjectTreeAfterReloadNoPanic(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.testSetSession(stored.ID, treeFakeSession{err: errors.New("枚举失败")})
	a.reloadTree()
	a.loadGroupsIn(stored, "")
	if got := a.treeErr[stored.ID]; got != "枚举失败" {
		t.Fatalf("treeErr = %q", got)
	}
}

func TestObjectTreeWithoutSessionDrawsNothing(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "other", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.reloadTree()
	a.loadGroupsIn(stored, "")
	if got := a.treeErr[stored.ID]; got != "" {
		t.Fatalf("没连上不该写错误行（点已经是灰的）: %q", got)
	}
	if _, ok := a.treeData[stored.ID]; ok {
		t.Fatal("must not cache another connection's objects")
	}
}

func seedConnFileWithTable(t *testing.T, name, table string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "seed", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE "+table+" (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	return path
}

func TestDuplicateNameConnsIsolated(t *testing.T) {
	a := newSidebarRoot(t)
	pathA := seedConnFileWithTable(t, "a.db", "alpha")
	pathB := seedConnFileWithTable(t, "b.db", "beta")
	if err := a.store.Add(app.ConnProfile{Name: "db", Kind: "sqlite", Path: pathA}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(app.ConnProfile{Name: "db", Kind: "sqlite", Path: pathB}); err != nil {
		t.Fatal(err)
	}
	pa, pb := a.store.List()[0], a.store.List()[1]
	if pa.ID == pb.ID || pa.Name != pb.Name {
		t.Fatalf("setup: %+v %+v", pa, pb)
	}
	if err := a.openConn(pa); err != nil {
		t.Fatal(err)
	}
	a.reloadTree()
	a.loadGroupsIn(pa, "")
	if objs := a.treeData[pa.ID][engine.ObjectTable]; len(objs) != 1 || objs[0].Name != "alpha" {
		t.Fatalf("A cache: %+v", objs)
	}
	a.loadGroupsIn(pb, "")
	if got := a.treeErr[pb.ID]; got != "" {
		t.Fatalf("B 没连上时不该写错误行（圆点是灰的）: %q", got)
	}
	if _, ok := a.treeData[pb.ID]; ok {
		t.Fatal("must not cache A's objects under B's ID")
	}
	a.treeExpand[pb.ID] = true
	a.expandConn(pb)
	if !a.connected(pb.ID) || !a.connected(pa.ID) {
		t.Fatal("expand must connect B, and A must stay connected")
	}
	a.reloadTree()
	a.loadGroupsIn(pb, "")
	if err := a.treeErr[pb.ID]; err != "" {
		t.Fatalf("B load after reconnect: %q", err)
	}
	if objs := a.treeData[pb.ID][engine.ObjectTable]; len(objs) != 1 || objs[0].Name != "beta" {
		t.Fatalf("B cache must be B's objects: %+v", objs)
	}
	if _, ok := a.treeData[pa.ID]; ok {
		t.Fatal("must not overwrite A's cache while loading B")
	}
}

func TestFailedOpenSetsTreeErrAndRemoveClears(t *testing.T) {
	a := newSidebarRoot(t)
	goodPath := seedConnFile(t)
	badPath := filepath.Join(t.TempDir(), "willvanish.db")
	if err := os.WriteFile(badPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(app.ConnProfile{Name: "good", Kind: "sqlite", Path: goodPath}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(app.ConnProfile{Name: "bad", Kind: "sqlite", Path: badPath}); err != nil {
		t.Fatal(err)
	}
	good, bad := a.store.List()[0], a.store.List()[1]
	if err := a.openConn(good); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(badPath); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(bad); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErrOf(bad.ID) == "" || a.connErr == "" {
		t.Fatalf("失败原因要记在数据源名下: %q / %q", a.connErrOf(bad.ID), a.connErr)
	}
	if !a.connected(good.ID) {
		t.Fatal("failed open must keep previous session")
	}
	a.removeConn(bad.ID)
	if a.connErrOf(bad.ID) != "" {
		t.Fatalf("remove of failed profile must clear residue: %q", a.connErrOf(bad.ID))
	}
	if _, ok := a.store.Get(bad.ID); ok {
		t.Fatal("bad profile must be removed")
	}
	if !a.connected(good.ID) {
		t.Fatal("good session must survive removing bad profile")
	}
}

func TestExpandRetryClearsStickyTreeErr(t *testing.T) {
	a := newSidebarRoot(t)
	path := filepath.Join(t.TempDir(), "retry.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(app.ConnProfile{Name: "retry", Kind: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	p := a.store.List()[0]
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.toggleExpandConn(p)
	if !a.treeExpand[p.ID] {
		t.Fatal("toggle must expand")
	}
	// 连不上时不留错误行（树里不再写「未连接」），改为圆点变红 + 记失败原因
	if got := a.connErrOf(p.ID); got == "" {
		t.Fatal("failed expand must record the failure for the red dot")
	}
	if got := a.treeErr[p.ID]; got != "" {
		t.Fatalf("failed expand must not write a tree error line: %q", got)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := engine.Open(engine.Profile{Kind: "sqlite", Name: "retry", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunStatement(t.Context(), "CREATE TABLE recovered (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	a.toggleExpandConn(p)
	if a.treeExpand[p.ID] {
		t.Fatal("second toggle must collapse")
	}
	a.toggleExpandConn(p)
	if a.treeErr[p.ID] != "" {
		t.Fatalf("re-expand must clear sticky treeErr, got %q", a.treeErr[p.ID])
	}
	if !a.connected(p.ID) {
		t.Fatalf("retry must open: %d sessions", len(a.sessions))
	}
	a.reloadTree()
	a.loadGroupsIn(p, "")
	if got := a.treeData[p.ID][engine.ObjectTable]; len(got) != 1 || got[0].Name != "recovered" {
		t.Fatalf("recovered objects: %+v err=%q", got, a.treeErr[p.ID])
	}
}

func TestSidebarMySQLConnFlow(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("MySQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "mysql" {
		t.Fatalf("kind toggle: %q", a.connFormKind)
	}
	a.connFormName = "远程库"
	a.connFormDB = "app"
	a.connFormHost = "10.0.0.1"
	a.connFormPort = "3306"
	a.connFormUser = "root"
	a.connFormPass = "secret"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.connFormOpen {
		t.Fatalf("save must close modal: %s", a.connFormErr)
	}
	stored := a.store.List()
	if len(stored) != 1 {
		t.Fatalf("store: %+v", stored)
	}
	p := stored[0]
	if p.Kind != "mysql" || p.Host != "10.0.0.1" || p.Port != 3306 || p.User != "root" || p.Password != "secret" || p.Database != "app" {
		t.Fatalf("stored: %+v", p)
	}
	tt.Frame()
	if err := tt.Click("编辑连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.connFormOpen || a.connFormKind != "mysql" || a.connFormHost != "10.0.0.1" || a.connFormPort != "3306" || a.connFormPass != "secret" {
		t.Fatalf("edit refill: kind=%q host=%q port=%q pass=%q", a.connFormKind, a.connFormHost, a.connFormPort, a.connFormPass)
	}
}

func TestOpenConnMySQLProfileFields(t *testing.T) {
	var captured engine.Profile
	engine.Register("mysql", func(p engine.Profile) (engine.Session, error) {
		captured = p
		return treeFakeSession{}, nil
	})
	a := newSidebarRoot(t)
	p := app.ConnProfile{
		ID: "m1", Name: "远程", Kind: "mysql",
		Host: "db.local", Port: 3307, User: "u", Password: "pw", Database: "app",
	}
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	if captured.Kind != "mysql" || captured.Name != "远程" || captured.Host != "db.local" || captured.Port != 3307 ||
		captured.User != "u" || captured.Password != "pw" || captured.Database != "app" {
		t.Fatalf("profile mapping: %+v", captured)
	}
	if !a.connected(p.ID) {
		t.Fatal("openConn must install session")
	}
}

func TestSidebarConsolesListOpenState(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	if _, err := m.Create(prof.ID, "开着的"); err != nil {
		t.Fatal(err)
	}
	closed, _ := m.Create(prof.ID, "关掉的")
	_ = m.CloseConsole(closed.ID)
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("开着的") || !tt.HasText("关掉的") {
		t.Fatalf("consoles missing: %q", tt.Texts())
	}
}

func TestSidebarReopenClosedConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "草稿")
	m.SetText(rec.ID, "SELECT 5")
	_ = m.Flush(rec.ID)
	_ = m.CloseConsole(rec.ID)
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("草稿"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.IsOpen(rec.ID) {
		t.Fatal("clicking a closed console must reopen it")
	}
	if got := a.activeText(); got != "SELECT 5" {
		t.Fatalf("buffer after reopen: %q", got)
	}
}

func TestSidebarCreateConsoleWithoutConnection(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if len(a.sessions) != 0 {
		t.Fatal("precondition: not connected")
	}
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建控制台"); err != nil {
		t.Fatal("must be clickable without a connection:", err)
	}
	if len(a.consoleMgr().ConsolesOf(prof.ID)) != 1 {
		t.Fatal("console must be created without connecting")
	}
}

func TestRemoveConnDeletesConsoleFiles(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, _ := a.consoleMgr().Create(prof.ID, "c")
	files := app.ConsoleFiles{Dir: a.store.ConsolesDir()}
	if err := files.Write(rec.ID, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	a.removeConn(prof.ID)
	if files.Exists(rec.ID) {
		t.Fatal(".sql must be removed with the connection")
	}
	if len(a.consoleMgr().Open()) != 0 {
		t.Fatal("tabs must be gone")
	}
	if a.activeConsole != "" {
		t.Fatalf("active console must clear: %q", a.activeConsole)
	}
}

func TestSidebarRenameConsoleFlow(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "旧名")
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("旧名"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("重命名控制台") {
		t.Fatalf("rename modal missing: %q", tt.Texts())
	}
	a.modalText = "新名"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, _ := a.store.GetConsole(rec.ID); got.Name != "新名" {
		t.Fatalf("renamed: %+v", got)
	}
	if !tt.HasText("新名") || tt.HasText("旧名") {
		t.Fatalf("sidebar/tab not refreshed: %q", tt.Texts())
	}
}

func TestSidebarDeleteConsoleFlow(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	m := a.consoleMgr()
	rec, _ := m.Create(prof.ID, "要删的")
	files := app.ConsoleFiles{Dir: a.store.ConsolesDir()}
	if err := files.Write(rec.ID, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.RightClick("要删的"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := a.store.GetConsole(rec.ID); ok {
		t.Fatal("console must be deleted")
	}
	if files.Exists(rec.ID) {
		t.Fatal(".sql must be deleted")
	}
	if a.activeConsole != "" {
		t.Fatalf("active must clear: %q", a.activeConsole)
	}
}

type treeSchemaFake struct{ treeFakeSession }

func (f treeSchemaFake) Schemas(ctx context.Context) ([]string, error) {
	return []string{"app", "public"}, nil
}

func (f treeSchemaFake) ObjectsIn(ctx context.Context, schema string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	if kind != engine.ObjectTable {
		return nil, nil
	}
	return []engine.DBObject{{Kind: kind, Name: schema + "_t", Schema: schema}}, nil
}

func TestSchemaTreeRendersSchemasThenObjects(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "pg", Kind: "postgres", Host: "h", Port: 5432}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.testSetSession(stored.ID, treeSchemaFake{})
	a.treeExpand[stored.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	for _, want := range []string{"▣ app", "▣ public"} {
		if !tt.HasText(want) {
			t.Fatalf("schema %q missing: %q", want, tt.Texts())
		}
	}
	// 展开 app schema → 出现它的表（测试里直接置展开态，再走两帧让懒加载完成）
	*a.groupOpenPtr(stored.ID+"|app|schema", true) = true
	tt.Frame()
	*a.groupOpenPtr(stored.ID+"|app|"+kindKey(engine.ObjectTable), true) = true
	tt.Frame()
	tt.Frame()
	if !tt.HasText("app_t") {
		t.Fatalf("object under schema missing: %q", tt.Texts())
	}
}

func TestQualifiedPreviewName(t *testing.T) {
	got := qualifiedName("postgres", engine.DBObject{Kind: engine.ObjectTable, Name: "t", Schema: "app"})
	if got != `"app"."t"` {
		t.Fatalf("qualified: %q", got)
	}
	if got := qualifiedName("sqlite", engine.DBObject{Name: "t"}); got != `"t"` {
		t.Fatalf("unqualified: %q", got)
	}
}

func TestConnFormLeftRightNav(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 900)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"SQLite", "MySQL", "PostgreSQL", "ClickHouse"} {
		if !tt.HasText(label) {
			t.Fatalf("kind nav missing %q: %q", label, tt.Texts())
		}
	}
	if err := tt.Click("PostgreSQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormKind != "postgres" {
		t.Fatalf("kind: %q", a.connFormKind)
	}
	if a.connFormPort != "5432" {
		t.Fatalf("default port must follow the kind: %q", a.connFormPort)
	}
	if !tt.HasText("PostgreSQL 连接") {
		t.Fatalf("right pane title missing: %q", tt.Texts())
	}
	// 切到 ClickHouse：端口仍跟随新默认值
	if err := tt.Click("ClickHouse"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.connFormPort != "9000" {
		t.Fatalf("clickhouse default port: %q", a.connFormPort)
	}
}

func TestConnKindIcons(t *testing.T) {
	seen := map[ui.Color]bool{}
	for _, k := range connKinds {
		if connKindIcon(k.kind) == nil {
			t.Fatalf("icon missing for %q", k.kind)
		}
		col := connKindColor(k.kind)
		if seen[col] {
			t.Fatalf("duplicate brand color for %q", k.kind)
		}
		seen[col] = true
	}
	if connKindIcon("oracle") != nil {
		t.Fatal("unknown kind must have no icon")
	}
}

// rectHasColor 在矩形里找是否有指定颜色（用于确认品牌图标真的画出来了）。
func rectHasColor(img *image.RGBA, r ui.Rect, want ui.Color) bool {
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > img.Bounds().Dx() {
		x1 = img.Bounds().Dx()
	}
	if y1 > img.Bounds().Dy() {
		y1 = img.Bounds().Dy()
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if c.R == want.R && c.G == want.G && c.B == want.B {
				return true
			}
		}
	}
	return false
}

func TestSidebarFolderTreeAndIndent(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	root, err := a.store.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := a.store.AddFolder("订单", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := app.ConnProfile{Name: "订单库", Kind: "sqlite", Path: path, FolderID: sub.ID}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	for _, want := range []string{"生产", "订单", "订单库"} {
		if !tt.HasText(want) {
			t.Fatalf("folder tree missing %q: %q", want, tt.Texts())
		}
	}
	rootRect, _ := tt.Find("生产")
	subRect, _ := tt.Find("订单")
	connRect, _ := tt.Find("订单库")
	if !(subRect.X > rootRect.X && connRect.X > subRect.X) {
		t.Fatalf("indent not applied: root=%v sub=%v conn=%v", rootRect, subRect, connRect)
	}
	// 连接名左边应该能看到品牌色图标。
	iconArea := ui.Rect{X: connRect.X - 44, Y: connRect.Y, W: 44, H: connRect.H}
	if !rectHasColor(tt.Image(), iconArea, connKindColor("sqlite")) {
		t.Fatalf("sqlite brand icon not painted in %v", iconArea)
	}

	// 收起根文件夹后，子文件夹和连接都不可见。
	if err := tt.Click("收起 生产"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("订单") || tt.HasText("订单库") {
		t.Fatalf("collapsed folder still shows children: %q", tt.Texts())
	}
	if err := tt.Click("展开 生产"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("订单库") {
		t.Fatalf("expanded folder must show connection: %q", tt.Texts())
	}
}

func TestSidebarFolderCreateRenameDelete(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()

	if err := tt.Click("新建文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.folderModalText = "生产"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	folders := a.store.Folders()
	if len(folders) != 1 || folders[0].Name != "生产" {
		t.Fatalf("folder not created: %+v", folders)
	}
	if !tt.HasText("生产") {
		t.Fatalf("folder row missing: %q", tt.Texts())
	}

	// 往文件夹里放一个连接，删除文件夹后连接应留在根级。
	p := app.ConnProfile{Name: "订单库", Kind: "sqlite", Path: path, FolderID: folders[0].ID}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if err := tt.RightClick("生产"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.folderModalText = "线上"
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.store.Folders(); len(got) != 1 || got[0].Name != "线上" {
		t.Fatalf("folder not renamed: %+v", got)
	}

	if err := tt.RightClick("线上"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.store.Folders()) != 0 {
		t.Fatalf("folder not deleted: %+v", a.store.Folders())
	}
	stored := a.store.List()
	if len(stored) != 1 || stored[0].FolderID != "" {
		t.Fatalf("connection must move to root: %+v", stored)
	}
	if !tt.HasText("订单库") {
		t.Fatalf("connection disappeared after folder delete: %q", tt.Texts())
	}
}

func TestSidebarConnFormFolderSelect(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	f, err := a.store.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("位置") {
		t.Fatalf("folder select missing in conn form: %q", tt.Texts())
	}
	a.connFormName = "订单库"
	a.connFormPath = path
	a.connFormFolder = f.ID
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	stored := a.store.List()
	if len(stored) != 1 || stored[0].FolderID != f.ID {
		t.Fatalf("connection folder not saved: %+v", stored)
	}
	folderRect, _ := tt.Find("生产")
	connRect, _ := tt.Find("订单库")
	if connRect.X <= folderRect.X {
		t.Fatalf("connection under folder must be indented: folder=%v conn=%v", folderRect, connRect)
	}
}

func TestConnFormFolderSelectChoosesFolder(t *testing.T) {
	a := newSidebarRoot(t)
	root, _ := a.store.AddFolder("生产", "")
	sub, err := a.store.AddFolder("订单", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// 打开「位置」下拉，选嵌套文件夹（唯一标签，避免和侧栏的「订单」混淆）。
	if err := tt.Click("根目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("生产 / 订单"); err != nil {
		t.Fatalf("choose folder: %v (texts %q)", err, tt.Texts())
	}
	tt.Frame()
	if a.connFormFolder != sub.ID {
		t.Fatalf("folder select = %q, want %q", a.connFormFolder, sub.ID)
	}
	p, err := a.connFormProfile()
	if err != nil {
		t.Fatal(err)
	}
	if p.FolderID != sub.ID {
		t.Fatalf("profile folder = %q", p.FolderID)
	}
}

// dragRow 按住 Ctrl 从 from 拖到 to 的指定纵向位置（相对文本矩形）。
func dragRowCtrl(tt *ui.Tester, from, to ui.Rect, toY float32) {
	tt.HoldModifiers(ui.Ctrl)
	tt.Frame()
	sx, sy := from.X+from.W/2, from.Y+from.H/2
	tx := to.X + to.W/2
	tt.Press(sx, sy)
	// Tester 的 Press 不带修饰键（真实窗口的按下事件会带上 Ctrl）。
	// 按下后重新按住 Ctrl，让下一帧把行接成拖拽源，再移动。
	tt.HoldModifiers(ui.Ctrl)
	tt.Frame()
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(sx+(tx-sx)*f, sy+(toY-sy)*f)
	}
	tt.Release(tx, toY)
	tt.Frame()
	tt.HoldModifiers(0)
	tt.Frame()
}

func TestSidebarDragFolderRequiresCtrl(t *testing.T) {
	a := newSidebarRoot(t)
	fa, _ := a.store.AddFolder("A", "")
	fb, _ := a.store.AddFolder("B", "")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	ra, _ := tt.Find("A")
	rb, _ := tt.Find("B")

	// 不按 Ctrl：普通拖动不应改变结构。
	sx, sy := rb.X+rb.W/2, rb.Y+rb.H/2
	tx, ty := ra.X+ra.W/2, ra.Y+ra.H/2
	tt.Press(sx, sy)
	tt.Move(tx, ty)
	tt.Release(tx, ty)
	tt.Frame()
	if got, _ := folderByID(t, a, fb.ID); got.ParentID != "" {
		t.Fatalf("drag without Ctrl moved folder: %+v", got)
	}

	// 按住 Ctrl 拖到 A 中间：B 移入 A。
	dragRowCtrl(tt, rb, ra, ra.Y+ra.H/2)
	got, _ := folderByID(t, a, fb.ID)
	if got.ParentID != fa.ID {
		t.Fatalf("Ctrl+drag into folder: %+v", got)
	}
}

func folderByID(t *testing.T, a *root, id string) (app.ConnFolder, bool) {
	t.Helper()
	for _, f := range a.store.Folders() {
		if f.ID == id {
			return f, true
		}
	}
	return app.ConnFolder{}, false
}

func TestSidebarDragSortConnections(t *testing.T) {
	dir := t.TempDir()
	a := newSidebarRoot(t)
	mk := func(name, file string) string {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		p := app.ConnProfile{Name: name, Kind: "sqlite", Path: path}
		if err := a.store.Add(p); err != nil {
			t.Fatal(err)
		}
		return a.store.List()[len(a.store.List())-1].ID
	}
	mk("Alpha", "a.db")
	beta := mk("Beta", "b.db")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	ra, _ := tt.Find("Alpha")
	rb, _ := tt.Find("Beta")

	// 拖 Beta 到 Alpha 的上沿：插到前面。
	dragRowCtrl(tt, rb, ra, ra.Y+1)
	list := a.store.List()
	if len(list) != 2 || list[0].ID != beta {
		t.Fatalf("sort connections: %+v", list)
	}
	rb2, _ := tt.Find("Beta")
	ra2, _ := tt.Find("Alpha")
	if rb2.Y >= ra2.Y {
		t.Fatalf("rendered order: Beta.Y=%v Alpha.Y=%v", rb2.Y, ra2.Y)
	}
}

func TestSidebarDragSortFolders(t *testing.T) {
	a := newSidebarRoot(t)
	if _, err := a.store.AddFolder("A", ""); err != nil {
		t.Fatal(err)
	}
	fb, _ := a.store.AddFolder("B", "")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	ra, _ := tt.Find("A")
	rb, _ := tt.Find("B")
	dragRowCtrl(tt, rb, ra, ra.Y+1)
	folders := a.store.Folders()
	if len(folders) != 2 || folders[0].ID != fb.ID {
		t.Fatalf("sort folders: %+v", folders)
	}
}

func TestSidebarDragFolderCycleRejected(t *testing.T) {
	a := newSidebarRoot(t)
	fa, _ := a.store.AddFolder("A", "")
	fb, err := a.store.AddFolder("B", fa.ID)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	ra, _ := tt.Find("A")
	rb, _ := tt.Find("B")
	// 把 A 拖进自己的子文件夹 B：必须拒绝，A 仍在根、B 仍是 A 的子级。
	dragRowCtrl(tt, ra, rb, rb.Y+rb.H/2)
	gotA, _ := folderByID(t, a, fa.ID)
	gotB, _ := folderByID(t, a, fb.ID)
	if gotA.ParentID != "" || gotB.ParentID != fa.ID {
		t.Fatalf("cycle drop changed tree: A=%+v B=%+v", gotA, gotB)
	}
}

func TestSidebarFolderRowClickToggles(t *testing.T) {
	path := seedConnFile(t)
	a := newSidebarRoot(t)
	f, err := a.store.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(app.ConnProfile{Name: "订单库", Kind: "sqlite", Path: path, FolderID: f.ID}); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("生产"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("订单库") {
		t.Fatalf("clicking folder row should collapse it: %q", tt.Texts())
	}
	if err := tt.Click("生产"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("订单库") {
		t.Fatalf("clicking folder row again should expand: %q", tt.Texts())
	}
}
