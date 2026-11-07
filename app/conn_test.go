package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *ConnStore {
	t.Helper()
	return LoadStore(filepath.Join(t.TempDir(), "connections.json"))
}

func mustAdd(t *testing.T, s *ConnStore, name, path string) ConnProfile {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	p := ConnProfile{Name: name, Kind: "sqlite", Path: path}
	if err := s.Add(p); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) == 0 {
		t.Fatalf("added profile %q not found", name)
	}
	p.ID = list[len(list)-1].ID
	got, ok := s.Get(p.ID)
	if !ok {
		t.Fatalf("added profile %q not found", name)
	}
	return got
}

func TestStoreAddRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	db := filepath.Join(dir, "a.db")
	p := mustAdd(t, s, "本地库", db)
	if p.ID == "" {
		t.Fatal("Add must assign an ID")
	}
	if len(s.List()) != 1 {
		t.Fatalf("list = %v", s.List())
	}

	s2 := LoadStore(path)
	if len(s2.List()) != 1 || s2.List()[0].Name != "本地库" || s2.List()[0].ID != p.ID {
		t.Fatalf("reload: %+v", s2.List())
	}
}

func TestStoreAddValidation(t *testing.T) {
	dir := t.TempDir()
	s := LoadStore(filepath.Join(dir, "c.json"))
	if err := s.Add(ConnProfile{Name: "", Kind: "sqlite", Path: filepath.Join(dir, "x.db")}); err == nil {
		t.Fatal("empty name must fail")
	}
	if err := s.Add(ConnProfile{Name: "n", Kind: "sqlite", Path: filepath.Join(dir, "missing.db")}); err == nil {
		t.Fatal("missing path must fail")
	}
	if err := s.Add(ConnProfile{Name: "n", Kind: "mysql", Path: dir}); err == nil {
		t.Fatal("non-sqlite kind must fail in M2")
	}
	ok := mustAdd(t, s, "dup", filepath.Join(dir, "b.db"))
	if err := s.Add(ConnProfile{ID: ok.ID, Name: "clash", Kind: "sqlite", Path: ok.Path}); err == nil {
		t.Fatal("explicit duplicate ID must fail")
	}
}

func TestStoreUpdateRemoveActive(t *testing.T) {
	dir := t.TempDir()
	s := LoadStore(filepath.Join(dir, "c.json"))
	p := mustAdd(t, s, "one", filepath.Join(dir, "a.db"))
	p.Name = "renamed"
	if err := s.Update(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(p.ID)
	if got.Name != "renamed" {
		t.Fatalf("update: %+v", got)
	}
	s.SetActive(p.ID)
	if act, ok := s.Active(); !ok || act.ID != p.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	if err := s.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Active(); ok {
		t.Fatal("removing active connection must clear activeID")
	}
	if len(s.List()) != 0 {
		t.Fatal("list must be empty")
	}
	s2 := LoadStore(filepath.Join(dir, "c.json"))
	if len(s2.List()) != 0 {
		t.Fatal("removal must persist")
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 0 {
		t.Fatal("corrupt file must start empty")
	}
	if !strings.Contains(s.Warning(), "损坏") {
		t.Fatalf("warning = %q", s.Warning())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt file must be renamed away, stat = %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("corrupt file must be kept as .bak")
	}
	s2 := LoadStore(path)
	if s2.Warning() != "" {
		t.Fatalf("reload warning = %q", s2.Warning())
	}
}

func TestStoreUpdateUnknownID(t *testing.T) {
	s := tempStore(t)
	if err := s.Update(ConnProfile{ID: "nope", Name: "n", Kind: "sqlite", Path: os.TempDir()}); err == nil {
		t.Fatal("update unknown ID must fail")
	}
	if err := s.Remove("nope"); err == nil {
		t.Fatal("remove unknown ID must fail")
	}
}

func TestStoreSetActivePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	p := mustAdd(t, s, "one", filepath.Join(dir, "a.db"))
	s.SetActive(p.ID)
	if act, ok := s.Active(); !ok || act.ID != p.ID {
		t.Fatalf("active: %+v %v", act, ok)
	}
	s2 := LoadStore(path)
	if act, ok := s2.Active(); !ok || act.ID != p.ID {
		t.Fatalf("reload active: %+v %v", act, ok)
	}
}

func TestStoreSetActiveSaveFailureRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	p := mustAdd(t, s, "one", filepath.Join(dir, "a.db"))
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	s.SetActive(p.ID)
	if _, ok := s.Active(); ok {
		t.Fatal("activeID must roll back after save failure")
	}
	if !strings.Contains(s.Warning(), "保存活动连接失败") {
		t.Fatalf("warning = %q", s.Warning())
	}
}

func TestStoreValidateProfileByKind(t *testing.T) {
	s := LoadStore(filepath.Join(t.TempDir(), "c.json"))
	dir := t.TempDir()
	if err := s.Add(ConnProfile{Name: "d", Kind: "sqlite", Path: dir}); err == nil {
		t.Fatal("sqlite directory path must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql"}); err == nil {
		t.Fatal("mysql without host must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 0}); err == nil {
		t.Fatal("mysql port 0 must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 70000}); err == nil {
		t.Fatal("mysql port out of range must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "x", Kind: "oracle", Host: "h", Port: 1521}); err == nil {
		t.Fatal("unknown kind must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "pg", Kind: "postgres", Host: "h", Port: 5432}); err != nil {
		t.Fatalf("valid postgres profile: %v", err)
	}
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 3306, User: "root", Password: "s", Database: "app"}); err != nil {
		t.Fatalf("valid mysql profile: %v", err)
	}
	list := s.List()
	if len(list) != 2 || list[1].Kind != "mysql" || list[1].Port != 3306 || list[1].Password != "s" {
		t.Fatalf("round-trip: %+v", list)
	}
}

func TestStoreLoadsLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(`{"conns":[{"id":"a","name":"x","kind":"sqlite","path":"/tmp/x.db"}],"activeID":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 1 || s.List()[0].Name != "x" || s.List()[0].Host != "" || s.List()[0].Port != 0 {
		t.Fatalf("legacy load: %+v", s.List())
	}
}

func TestConsolesRoundTrip(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, err := s.CreateConsole(p.ID, "  查询  ")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "查询" || rec.ConnectionID != p.ID || rec.FilePath != filepath.Join(s.ConsolesDir(), p.ID+"-"+rec.ID+".sql") {
		t.Fatalf("bad record: %+v", rec)
	}
	again := LoadStore(s.path)
	got, ok := again.GetConsole(rec.ID)
	if !ok || got.Name != "查询" || got.SortOrder != rec.SortOrder {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
}

func TestCreateConsoleDefaults(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	first, err := s.CreateConsole(p.ID, "")
	if err != nil || first.Name != "控制台 1" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := s.CreateConsole(p.ID, "  ")
	if err != nil || second.Name != "控制台 2" || second.SortOrder != first.SortOrder+1 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if _, err := s.CreateConsole("nope", "x"); err == nil {
		t.Fatal("unknown connection must fail")
	}
}

func TestConsolesLegacyJSONWithoutField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	legacy := `{"conns":[{"id":"abc","name":"旧","kind":"sqlite","path":"/tmp/x.db"}],"activeID":"abc"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if len(s.List()) != 1 || len(s.ListConsoles("abc")) != 0 {
		t.Fatalf("legacy load: %+v", s.List())
	}
	if _, err := s.CreateConsole("abc", "新"); err != nil {
		t.Fatal(err)
	}
	re := LoadStore(path)
	if len(re.List()) != 1 || len(re.ListConsoles("abc")) != 1 {
		t.Fatalf("legacy rewrite lost data: conns=%d consoles=%d", len(re.List()), len(re.ListConsoles("abc")))
	}
}

func TestConsoleCaretAndDelete(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	rec, _ := s.CreateConsole(p.ID, "c")
	s.SetConsoleCaret(rec.ID, 3, 7)
	s.TouchConsole(rec.ID, 1234)
	got, _ := LoadStore(s.path).GetConsole(rec.ID)
	if got.CaretStart != 3 || got.CaretEnd != 7 || got.UpdatedAt != 1234 {
		t.Fatalf("caret/updated: %+v", got)
	}
	if err := s.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadStore(s.path).GetConsole(rec.ID); ok {
		t.Fatal("console must be deleted")
	}
}

func TestWorkspaceCRUDRoundTrip(t *testing.T) {
	s := tempStore(t)
	ws, err := s.CreateWorkspace("")
	if err != nil || ws.Name != "默认" || !ws.AutoNamed {
		t.Fatalf("default ws: %+v %v", ws, err)
	}
	named, err := s.CreateWorkspace("分析")
	if err != nil || named.AutoNamed || named.SortOrder != ws.SortOrder+1 {
		t.Fatalf("named ws: %+v %v", named, err)
	}
	if err := s.RenameWorkspace(ws.ID, "日常"); err != nil {
		t.Fatal(err)
	}
	s.SetActiveWorkspace(named.ID)
	again := LoadStore(s.path)
	if got, _ := again.GetWorkspace(ws.ID); got.Name != "日常" {
		t.Fatalf("rename lost: %+v", got)
	}
	if again.ActiveWorkspaceID() != named.ID {
		t.Fatalf("active ws: %q", again.ActiveWorkspaceID())
	}
	if err := s.DeleteWorkspace(named.ID); err != nil {
		t.Fatal(err)
	}
	if LoadStore(s.path).ActiveWorkspaceID() != "" {
		t.Fatal("deleting active workspace must clear pointer")
	}
}

func TestWorkspaceMembersAndLastActive(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	a, _ := s.CreateConsole(p.ID, "a")
	b, _ := s.CreateConsole(p.ID, "b")
	if err := s.AddWorkspaceMember(ws.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkspaceMember(ws.ID, a.ID); err != nil {
		t.Fatal("add must be idempotent:", err)
	}
	_ = s.AddWorkspaceMember(ws.ID, b.ID)
	if err := s.SetWorkspaceLastActive(ws.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 2 || got.Members[0] != a.ID || got.LastActiveConsoleID != b.ID {
		t.Fatalf("members: %+v", got)
	}
	_ = s.RemoveWorkspaceMember(ws.ID, a.ID)
	got, _ = LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 1 || got.Members[0] != b.ID {
		t.Fatalf("remove: %+v", got.Members)
	}
}

func TestDeleteConsoleStripsMembership(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	_ = s.SetWorkspaceLastActive(ws.ID, rec.ID)
	if err := s.DeleteConsole(rec.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 0 || got.LastActiveConsoleID != "" {
		t.Fatalf("membership not stripped: %+v", got)
	}
}

func TestDeleteWorkspaceKeepsConsoles(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	if err := s.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadStore(s.path).GetConsole(rec.ID); !ok {
		t.Fatal("deleting a workspace must keep consoles")
	}
}

func TestRemoveConnCascadesConsoles(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	rec, _ := s.CreateConsole(p.ID, "c")
	_ = s.AddWorkspaceMember(ws.ID, rec.ID)
	if err := s.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ListConsoles(p.ID)) != 0 {
		t.Fatal("consoles must be removed with the connection")
	}
	got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
	if len(got.Members) != 0 {
		t.Fatalf("members must be stripped: %+v", got.Members)
	}
}

func TestValidateProfilePostgresAndClickHouse(t *testing.T) {
	s := tempStore(t)
	if err := s.Add(ConnProfile{Name: "pg", Kind: "postgres", Host: "h", Port: 5432, User: "u", Database: "d"}); err != nil {
		t.Fatalf("valid postgres profile: %v", err)
	}
	if err := s.Add(ConnProfile{Name: "ck", Kind: "clickhouse", Host: "h", Port: 9000, User: "u", Database: "d"}); err != nil {
		t.Fatalf("valid clickhouse profile: %v", err)
	}
	if err := s.Add(ConnProfile{Name: "bad", Kind: "postgres", Port: 5432}); err == nil {
		t.Fatal("postgres without host must be rejected")
	}
	if err := s.Add(ConnProfile{Name: "bad2", Kind: "clickhouse", Host: "h", Port: 0}); err == nil {
		t.Fatal("clickhouse without port must be rejected")
	}
}

func TestFolderStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	root, err := s.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.AddFolder("订单", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := mustAdd(t, s, "订单库", filepath.Join(dir, "orders.db"))
	if err := s.MoveConnection(p.ID, child.ID); err != nil {
		t.Fatal(err)
	}

	s2 := LoadStore(path)
	folders := s2.Folders()
	if len(folders) != 2 || folders[0].Name != "生产" || folders[1].ParentID != root.ID {
		t.Fatalf("folders after reload: %+v", folders)
	}
	got, ok := s2.Get(p.ID)
	if !ok || got.FolderID != child.ID {
		t.Fatalf("connection folder after reload: %+v %v", got, ok)
	}
	if path := FolderPath(folders, child.ID); path != "生产 / 订单" {
		t.Fatalf("folder path = %q", path)
	}
}

func TestFolderDeleteMovesContents(t *testing.T) {
	s := tempStore(t)
	a, _ := s.AddFolder("A", "")
	b, _ := s.AddFolder("B", a.ID)
	c, _ := s.AddFolder("C", b.ID)
	p := mustAdd(t, s, "库", filepath.Join(t.TempDir(), "a.db"))
	if err := s.MoveConnection(p.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(p.ID)
	if got.FolderID != a.ID {
		t.Fatalf("connection must move to parent: %+v", got)
	}
	folders := s.Folders()
	for _, f := range folders {
		if f.ID == c.ID && f.ParentID != a.ID {
			t.Fatalf("child folder must move to parent: %+v", f)
		}
	}
	if err := s.DeleteFolder(a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(p.ID)
	if got.FolderID != "" {
		t.Fatalf("connection must move to root: %+v", got)
	}
}

func TestFolderDuplicateNames(t *testing.T) {
	s := tempStore(t)
	a, err := s.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFolder("生产", ""); err == nil {
		t.Fatal("same-level duplicate folder name must fail")
	}
	if _, err := s.AddFolder("生产", a.ID); err != nil {
		t.Fatalf("same name in another parent is fine: %v", err)
	}
	b, _ := s.AddFolder("测试", "")
	if err := s.RenameFolder(b.ID, "生产"); err == nil {
		t.Fatal("rename to existing sibling must fail")
	}
	if err := s.RenameFolder(b.ID, "预发"); err != nil {
		t.Fatal(err)
	}
}

func TestMoveConnection(t *testing.T) {
	s := tempStore(t)
	f, _ := s.AddFolder("生产", "")
	p := mustAdd(t, s, "库", filepath.Join(t.TempDir(), "a.db"))
	if err := s.MoveConnection(p.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.FolderID != f.ID {
		t.Fatalf("move into folder: %+v", got)
	}
	if err := s.MoveConnection(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.FolderID != "" {
		t.Fatalf("move to root: %+v", got)
	}
	if err := s.MoveConnection(p.ID, "nope"); err == nil {
		t.Fatal("unknown folder must fail")
	}
	if err := s.MoveConnection("nope", ""); err == nil {
		t.Fatal("unknown connection must fail")
	}
}

func TestFolderOrphanRefsNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	raw := `{
  "conns": [{"id":"c1","name":"c","kind":"sqlite","path":"x","folderID":"gone"}],
  "folders": [
    {"id":"f1","name":"A","parentID":"missing"},
    {"id":"f2","name":"B","parentID":"f2"}
  ]
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	folders := s.Folders()
	if len(folders) != 2 || folders[0].ParentID != "" || folders[1].ParentID != "" {
		t.Fatalf("orphan/cycle folders must return to root: %+v", folders)
	}
	got, _ := s.Get("c1")
	if got.FolderID != "" {
		t.Fatalf("orphan connection folder must return to root: %+v", got)
	}
}

func TestExportProfilesStripsFolderID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if _, err := ExportProfiles(path, []ConnProfile{{
		Name: "x", Kind: "sqlite", Path: "x.db", FolderID: "f1",
	}}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle ProfileBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Conns) != 1 || bundle.Conns[0].FolderID != "" {
		t.Fatalf("folder id must not be exported: %+v", bundle.Conns)
	}
}

func TestUpdateConnectionFolder(t *testing.T) {
	dir := t.TempDir()
	s := tempStore(t)
	f, err := s.AddFolder("生产", "")
	if err != nil {
		t.Fatal(err)
	}
	p := mustAdd(t, s, "库", filepath.Join(dir, "a.db"))
	p.FolderID = f.ID
	if err := s.Update(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.FolderID != f.ID {
		t.Fatalf("update folder: %+v", got)
	}
	p.FolderID = "missing"
	if err := s.Update(p); err == nil {
		t.Fatal("update to unknown folder must fail")
	}
	if got, _ := s.Get(p.ID); got.FolderID != f.ID {
		t.Fatalf("failed update must not change folder: %+v", got)
	}
}

func TestMoveConnectionToOrder(t *testing.T) {
	dir := t.TempDir()
	s := tempStore(t)
	a := mustAdd(t, s, "a", filepath.Join(dir, "a.db"))
	b := mustAdd(t, s, "b", filepath.Join(dir, "b.db"))
	c := mustAdd(t, s, "c", filepath.Join(dir, "c.db"))
	f, _ := s.AddFolder("F", "")

	if err := s.MoveConnectionTo(c.ID, "", a.ID); err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		var out []string
		for _, p := range s.List() {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(); len(got) != 3 || got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("reorder root: %v", got)
	}
	if err := s.MoveConnectionTo(b.ID, f.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(b.ID); got.FolderID != f.ID {
		t.Fatalf("move into folder: %+v", got)
	}
	if err := s.MoveConnectionTo(b.ID, "", c.ID); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "a" {
		t.Fatalf("move back before c: %v", got)
	}
	if err := s.MoveConnectionTo(a.ID, "", "missing"); err == nil {
		t.Fatal("unknown beforeID must fail")
	}
}

func TestMoveFolderToOrderAndNesting(t *testing.T) {
	s := tempStore(t)
	a, _ := s.AddFolder("A", "")
	b, _ := s.AddFolder("B", "")
	c, _ := s.AddFolder("C", "")

	if err := s.MoveFolderTo(c.ID, "", b.ID); err != nil {
		t.Fatal(err)
	}
	rootNames := func() []string {
		var out []string
		for _, f := range s.Folders() {
			if f.ParentID == "" {
				out = append(out, f.Name)
			}
		}
		return out
	}
	if got := rootNames(); len(got) != 3 || got[0] != "A" || got[1] != "C" || got[2] != "B" {
		t.Fatalf("reorder folders: %v", got)
	}
	if err := s.MoveFolderTo(c.ID, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := rootNames(); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("nested folder still at root: %v", got)
	}
	got, _ := s.Folders(), s.Folders()
	for _, f := range got {
		if f.ID == c.ID && f.ParentID != a.ID {
			t.Fatalf("nested folder parent: %+v", f)
		}
	}
	if err := s.MoveFolderTo(a.ID, c.ID, ""); err == nil {
		t.Fatal("moving a folder into its descendant must fail")
	}
	if err := s.MoveFolderTo(a.ID, a.ID, ""); err == nil {
		t.Fatal("moving a folder into itself must fail")
	}
	if err := s.MoveFolderTo(b.ID, "", "missing"); err == nil {
		t.Fatal("unknown beforeID must fail")
	}
}

func TestMoveWorkspaceMember(t *testing.T) {
	s := tempStore(t)
	p := mustAdd(t, s, "本地", filepath.Join(t.TempDir(), "a.db"))
	ws, _ := s.CreateWorkspace("默认")
	a, _ := s.CreateConsole(p.ID, "a")
	b, _ := s.CreateConsole(p.ID, "b")
	c, _ := s.CreateConsole(p.ID, "c")
	for _, id := range []string{a.ID, b.ID, c.ID} {
		if err := s.AddWorkspaceMember(ws.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	members := func() []string {
		got, _ := LoadStore(s.path).GetWorkspace(ws.ID)
		return got.Members
	}
	// c 移到 a 前面
	if err := s.MoveWorkspaceMember(ws.ID, c.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if got := members(); len(got) != 3 || got[0] != c.ID || got[1] != a.ID {
		t.Fatalf("move before: %v", got)
	}
	// a 移到末尾（空 beforeID）
	if err := s.MoveWorkspaceMember(ws.ID, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := members(); got[2] != a.ID {
		t.Fatalf("move to end: %v", got)
	}
	// 位置不变：幂等
	if err := s.MoveWorkspaceMember(ws.ID, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveWorkspaceMember(ws.ID, c.ID, c.ID); err != nil {
		t.Fatal("move before itself is a no-op:", err)
	}
	if err := s.MoveWorkspaceMember(ws.ID, "nope", ""); err == nil {
		t.Fatal("unknown console must fail")
	}
	if err := s.MoveWorkspaceMember("nope", a.ID, ""); err == nil {
		t.Fatal("unknown workspace must fail")
	}
}

func TestStoreQueryTimeoutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := LoadStore(filepath.Join(dir, "connections.json"))
	p := mustAdd(t, s, "本地", filepath.Join(dir, "a.db"))
	p.QueryTimeout = 120
	if err := s.Update(p); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadStore(filepath.Join(dir, "connections.json")).Get(p.ID)
	if !ok || got.QueryTimeout != 120 {
		t.Fatalf("query timeout: %+v %v", got, ok)
	}
	bad := ConnProfile{Name: "x", Kind: "sqlite", Path: p.Path, QueryTimeout: -1}
	if err := s.Add(bad); err == nil {
		t.Fatal("negative query timeout must be rejected")
	}
	bad.QueryTimeout = 90000
	if err := s.Add(bad); err == nil {
		t.Fatal("query timeout over a day must be rejected")
	}
}
