package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplySyncPreservesDeviceState(t *testing.T) {
	dir := t.TempDir()
	store := LoadStore(filepath.Join(dir, "connections.json"))
	dbFile := filepath.Join(dir, "a.db")
	if err := os.WriteFile(dbFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c1 := ConnProfile{Name: "一", Kind: "sqlite", Path: dbFile}
	if err := store.Add(c1); err != nil {
		t.Fatal(err)
	}
	c1 = store.List()[0]
	c2 := ConnProfile{Name: "二", Kind: "sqlite", Path: dbFile}
	if err := store.Add(c2); err != nil {
		t.Fatal(err)
	}
	c2 = store.List()[1]
	folder, err := store.AddFolder("dev", "")
	if err != nil {
		t.Fatal(err)
	}
	s1, err := store.CreateConsole(c1.ID, "保留")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := store.CreateConsole(c1.ID, "删除")
	if err != nil {
		t.Fatal(err)
	}
	ext, err := store.CreateExternalConsole(c1.ID, "外部", filepath.Join(dir, "user.sql"))
	if err != nil {
		t.Fatal(err)
	}
	store.SetConsoleCaret(s1.ID, 3, 7)
	ws, err := store.CreateWorkspace("默认")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{s1.ID, s2.ID, ext.ID} {
		if err := store.AddWorkspaceMember(ws.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	store.SetActive(c1.ID)
	store.SetActiveWorkspace(ws.ID)

	incomingConns := []ConnProfile{
		{ID: c1.ID, Name: "一（远端）", Kind: "sqlite", Path: dbFile},
		{ID: "c3", Name: "三", Kind: "sqlite", Path: dbFile},
	}
	incomingConsoles := []ConsoleRecord{
		{ID: s1.ID, ConnectionID: c1.ID, Name: "保留（远端）", SortOrder: 1},
		{ID: "s3", ConnectionID: "c3", Name: "新控制台"},
	}
	incomingWS := []WorkspaceRecord{{ID: ws.ID, Name: "默认", Members: []string{s1.ID, "s3"}}}
	stats, err := store.ApplySync(incomingConns, []ConnFolder{{ID: folder.ID, Name: "dev2"}}, incomingConsoles, incomingWS)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Removed) != 1 || stats.Removed[0].ID != s2.ID {
		t.Fatalf("removed: %+v", stats.Removed)
	}
	if _, ok := store.Get(c2.ID); ok {
		t.Fatal("connection deleted remotely must disappear")
	}
	if p, ok := store.Get(c1.ID); !ok || p.Name != "一（远端）" {
		t.Fatalf("c1: %+v", p)
	}
	if p, ok := store.Get("c3"); !ok || p.Name != "三" {
		t.Fatalf("c3: %+v", p)
	}
	rec, ok := store.GetConsole(s1.ID)
	if !ok || rec.Name != "保留（远端）" || rec.CaretStart != 3 || rec.CaretEnd != 7 {
		t.Fatalf("console: %+v", rec)
	}
	if rec.FilePath != filepath.Join(store.ConsolesDir(), c1.ID+"-"+s1.ID+".sql") {
		t.Fatalf("file path: %q", rec.FilePath)
	}
	if _, ok := store.GetConsole(s2.ID); ok {
		t.Fatal("removed console must disappear")
	}
	if rec, ok := store.GetConsole(ext.ID); !ok || !rec.External {
		t.Fatalf("external console must be preserved: %+v", rec)
	}
	gotWS, _ := store.GetWorkspace(ws.ID)
	found := map[string]bool{}
	for _, id := range gotWS.Members {
		found[id] = true
	}
	if !found[s1.ID] || !found["s3"] {
		t.Fatalf("workspace members: %+v", gotWS.Members)
	}
	if active, ok := store.Active(); !ok || active.ID != c1.ID {
		t.Fatalf("active connection should stay when still present: %+v", active)
	}
	// 落盘后重新加载，确认写入的仍是本机 Vault 加密的密码。
	reloaded := LoadStore(filepath.Join(dir, "connections.json"))
	if p, ok := reloaded.Get(c1.ID); !ok || p.Password != "" || p.Name != "一（远端）" {
		t.Fatalf("reloaded: %+v", p)
	}
}
