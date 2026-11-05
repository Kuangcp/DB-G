package app

import (
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
	if rec.Name != "查询" || rec.ConnectionID != p.ID || rec.FilePath != filepath.Join(s.ConsolesDir(), rec.ID+".sql") {
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
