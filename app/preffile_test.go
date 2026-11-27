package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONPrefFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "prefs.json")
	want := struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}{Name: "db-g", N: 7}
	if err := saveJSONFile(path, want, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
	var got struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	found, err := loadJSONFile(path, &got)
	if err != nil || !found || got != want {
		t.Fatalf("load = %+v found=%v err=%v", got, found, err)
	}

	found, err = loadJSONFile(filepath.Join(t.TempDir(), "missing.json"), &got)
	if err != nil || found {
		t.Fatalf("missing: found=%v err=%v", found, err)
	}

	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := loadJSONFile(path, &got); !found || err == nil {
		t.Fatalf("corrupt: found=%v err=%v", found, err)
	}
}
