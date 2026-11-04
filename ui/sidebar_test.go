package ui

import (
	"context"
	"errors"
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
	if a.sess == nil {
		t.Fatal("openConn must install a session")
	}
	if a.connName != "本地库" || a.connErr != "" {
		t.Fatalf("connName=%q connErr=%q", a.connName, a.connErr)
	}
	if act, ok := a.store.Active(); !ok || act.ID != stored.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	a.ed.Buf.Set("SELECT name FROM users")
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.selStart, a.selEnd = 0, len([]rune("SELECT name FROM users"))
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
	good := app.ConnProfile{Name: "good", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(good); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(a.store.List()[0]); err != nil {
		t.Fatal(err)
	}
	bad := app.ConnProfile{Name: "bad", Kind: "sqlite", Path: filepath.Join(t.TempDir(), "gone.db")}
	if err := a.openConn(bad); err == nil {
		t.Fatal("missing file must fail")
	}
	if a.connErr == "" {
		t.Fatal("connErr must be set")
	}
	if a.sess == nil || a.connName != "good" {
		t.Fatal("failed open must keep previous session")
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
	if a.sess != nil || a.connName != "" {
		t.Fatalf("session=%v connName=%q", a.sess, a.connName)
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
	a.setSession(stored.ID, treeFakeSession{objs: []engine.DBObject{
		{Kind: engine.ObjectTable, Name: "users"},
	}})
	a.connName = stored.Name
	a.activeConnID = stored.ID
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
	a.setSession(stored.ID, treeFakeSession{err: errors.New("枚举失败")})
	a.connName = stored.Name
	a.activeConnID = stored.ID
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
	if a.sess == nil {
		a.expandConn(stored)
	}
	if a.sess == nil || a.connName != "auto" {
		t.Fatalf("expand of inactive conn must connect: session=%v name=%q", a.sess, a.connName)
	}
}

func TestObjectTreeAfterReloadNoPanic(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "lib", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.setSession(stored.ID, treeFakeSession{err: errors.New("枚举失败")})
	a.connName = stored.Name
	a.activeConnID = stored.ID
	a.reloadTree()
	a.loadGroups(stored)
	if got := a.treeErr[stored.ID]; got != "枚举失败" {
		t.Fatalf("treeErr = %q", got)
	}
}

func TestObjectTreeCrossConnBlocked(t *testing.T) {
	a := newSidebarRoot(t)
	p := app.ConnProfile{Name: "other", Kind: "sqlite", Path: seedConnFile(t)}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	stored := a.store.List()[0]
	a.setSession("", treeFakeSession{objs: []engine.DBObject{{Kind: engine.ObjectTable, Name: "users"}}})
	a.connName = "active-db"
	a.reloadTree()
	a.loadGroups(stored)
	if got := a.treeErr[stored.ID]; got != "未连接" {
		t.Fatalf("treeErr = %q", got)
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
	a.loadGroups(pa)
	if objs := a.treeData[pa.ID][engine.ObjectTable]; len(objs) != 1 || objs[0].Name != "alpha" {
		t.Fatalf("A cache: %+v", objs)
	}
	a.loadGroups(pb)
	if got := a.treeErr[pb.ID]; got != "未连接" {
		t.Fatalf("B must not reuse A's session, treeErr=%q", got)
	}
	if _, ok := a.treeData[pb.ID]; ok {
		t.Fatal("must not cache A's objects under B's ID")
	}
	a.treeExpand[pb.ID] = true
	a.expandConn(pb)
	if a.sess == nil || a.connName != "db" {
		t.Fatalf("expand must connect B: session=%v name=%q", a.sess, a.connName)
	}
	a.reloadTree()
	a.loadGroups(pb)
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
	if a.connErr == "" || a.failedConnID != bad.ID {
		t.Fatalf("connErr=%q failedConnID=%q", a.connErr, a.failedConnID)
	}
	if a.treeErr[bad.ID] == "" {
		t.Fatal("openConn failure must write treeErr under the failed profile ID")
	}
	if a.sess == nil || a.activeConnID != good.ID {
		t.Fatal("failed open must keep previous session")
	}
	a.removeConn(bad.ID)
	if a.connErr != "" || a.failedConnID != "" {
		t.Fatalf("remove of failed profile must clear residue: connErr=%q failedConnID=%q", a.connErr, a.failedConnID)
	}
	if _, ok := a.store.Get(bad.ID); ok {
		t.Fatal("bad profile must be removed")
	}
	if a.sess == nil || a.activeConnID != good.ID {
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
	if a.treeErr[p.ID] == "" {
		t.Fatal("failed expand must leave treeErr for the node")
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
	if a.sess == nil || a.activeConnID != p.ID {
		t.Fatalf("retry must open: session=%v active=%q", a.sess, a.activeConnID)
	}
	a.reloadTree()
	a.loadGroups(p)
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
	if err := tt.Click("编辑"); err != nil {
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
	if a.sess == nil || a.connName != "远程" {
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
	if got := a.ed.Buf.String(); got != "SELECT 5" {
		t.Fatalf("buffer after reopen: %q", got)
	}
}

func TestSidebarCreateConsoleWithoutConnection(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if a.sess != nil {
		t.Fatal("precondition: not connected")
	}
	a.treeExpand[prof.ID] = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("＋ 新建控制台"); err != nil {
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
