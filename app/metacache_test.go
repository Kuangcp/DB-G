package app

import (
	"os"
	"path/filepath"
	"testing"

	"db-g/engine"
)

func cacheProfile() ConnProfile {
	return ConnProfile{ID: "c1", Name: "本地", Kind: "postgres", Host: "db", Port: 5432,
		User: "u", Password: "secret", Database: "app", Protocol: "native", Params: "sslmode=require"}
}

func samplePayload() MetaCachePayload {
	return MetaCachePayload{
		Schemas: []string{"public", "audit"},
		Counts: map[string]map[engine.ObjectKind]int{
			"public": {engine.ObjectTable: 60000, engine.ObjectView: 4, engine.ObjectIndex: 0},
		},
		Objects: map[string]map[engine.ObjectKind][]engine.DBObject{
			"public": {
				engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "users", Schema: "public"}},
			},
		},
	}
}

func TestMetaCacheRoundTrip(t *testing.T) {
	c := NewMetaCache(filepath.Join(t.TempDir(), "meta"))
	p := cacheProfile()
	if err := c.Save(p, samplePayload()); err != nil {
		t.Fatal(err)
	}
	got, hit, stale := c.Load(p)
	if !hit || stale {
		t.Fatalf("hit=%v stale=%v", hit, stale)
	}
	if len(got.Schemas) != 2 || got.Schemas[1] != "audit" {
		t.Fatalf("schemas: %+v", got.Schemas)
	}
	if got.Counts["public"][engine.ObjectTable] != 60000 {
		t.Fatalf("counts: %+v", got.Counts)
	}
	if objs := got.Objects["public"][engine.ObjectTable]; len(objs) != 1 || objs[0].Name != "users" {
		t.Fatalf("objects: %+v", got.Objects)
	}
	if got.Fingerprint != MetaFingerprint(p) || got.SavedAt == 0 {
		t.Fatalf("metadata: %+v", got)
	}
}

func TestMetaCacheFingerprintMismatch(t *testing.T) {
	c := NewMetaCache(filepath.Join(t.TempDir(), "meta"))
	p := cacheProfile()
	if err := c.Save(p, samplePayload()); err != nil {
		t.Fatal(err)
	}
	// 名称/密码变化不影响缓存身份。
	q := p
	q.Name, q.Password = "改名", "other"
	if _, hit, _ := c.Load(q); !hit {
		t.Fatal("name/password must not invalidate metadata")
	}
	// 目标库变化视为未命中。
	q.Host = "other-db"
	if _, hit, _ := c.Load(q); hit {
		t.Fatal("host change must miss")
	}
}

func TestMetaCacheStaleAndCorrupt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "meta")
	c := NewMetaCacheWithTTL(dir, 0)
	p := cacheProfile()
	if err := c.Save(p, samplePayload()); err != nil {
		t.Fatal(err)
	}
	if _, hit, stale := c.Load(p); !hit || !stale {
		t.Fatalf("hit=%v stale=%v", hit, stale)
	}
	if err := os.WriteFile(c.MetaCachePath(p), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Load(p); hit {
		t.Fatal("corrupt cache must miss")
	}
}

func TestMetaCacheDelete(t *testing.T) {
	c := NewMetaCache(filepath.Join(t.TempDir(), "meta"))
	p := cacheProfile()
	if err := c.Save(p, samplePayload()); err != nil {
		t.Fatal(err)
	}
	c.Delete(p.ID)
	if _, hit, _ := c.Load(p); hit {
		t.Fatal("deleted cache must miss")
	}
	if err := c.Save(p, samplePayload()); err != nil {
		t.Fatal(err)
	}
	q := p
	q.ID = "c2"
	if err := c.Save(q, samplePayload()); err != nil {
		t.Fatal(err)
	}
	c.DeleteAll()
	if _, hit, _ := c.Load(p); hit {
		t.Fatal("DeleteAll must clear c1")
	}
	if _, hit, _ := c.Load(q); hit {
		t.Fatal("DeleteAll must clear c2")
	}
}

func TestMetaFingerprintStable(t *testing.T) {
	a := cacheProfile()
	b := a
	b.Name, b.Password, b.FolderID = "x", "y", "f"
	if MetaFingerprint(a) != MetaFingerprint(b) {
		t.Fatal("name/password/folder must not affect fingerprint")
	}
	b.Path = "/tmp/other.db"
	if MetaFingerprint(a) == MetaFingerprint(b) {
		t.Fatal("path must affect fingerprint")
	}
}

func TestMetaCacheOrderedWrites(t *testing.T) {
	c := NewMetaCache(filepath.Join(t.TempDir(), "meta"))
	p := cacheProfile()
	older := samplePayload()
	older.Schemas = []string{"old"}
	newer := samplePayload()
	newer.Schemas = []string{"new"}
	if err := c.SaveOrdered(p, newer, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveOrdered(p, older, 1); err != nil {
		t.Fatal(err)
	}
	got, hit, _ := c.Load(p)
	if !hit || len(got.Schemas) != 1 || got.Schemas[0] != "new" {
		t.Fatalf("older write must not overwrite: %+v", got.Schemas)
	}
	// 删除后，删除前发出的旧写入不能再落盘。
	c.Delete(p.ID)
	if err := c.SaveOrdered(p, older, 2); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Load(p); hit {
		t.Fatal("pending write after delete must be dropped")
	}
	if err := c.SaveOrdered(p, newer, 3); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Load(p); !hit {
		t.Fatal("new write after delete must land")
	}
}
