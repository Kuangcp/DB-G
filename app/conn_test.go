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
