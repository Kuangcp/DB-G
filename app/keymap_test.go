package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeymapRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	if got, warn := LoadKeymap(path); len(got) != 0 || warn != "" {
		t.Fatalf("缺失时该是空表: %v %q", got, warn)
	}
	want := map[string]string{"exec": "Ctrl+Enter, F5", "duplicateLine": "Ctrl+Shift+Y", "format": ""}
	if err := SaveKeymap(path, want); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadKeymap(path)
	if warn != "" {
		t.Fatal(warn)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("权限 %v", mode)
	}
}

func TestKeymapCorruptKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadKeymap(path)
	if len(got) != 0 || warn == "" {
		t.Fatalf("损坏时应回默认并告警: %v %q", got, warn)
	}
}
