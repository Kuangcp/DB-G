package ui

import (
	"os"
	"path/filepath"
	"testing"

	"db-g/app"
	"db-g/engine"
)

// 导入文件只做解析，不能碰 root（树/主题）；落地必须在 UI 线程单独一步。
func TestImportProfilesWorkDoesNotTouchUI(t *testing.T) {
	a := newConsoleRoot(t)
	a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{
		"keep": {engine.ObjectTable: {{Name: "t"}}},
	}
	src := filepath.Join(t.TempDir(), "profiles.json")
	dbPath := filepath.Join(t.TempDir(), "a.db")
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	want := app.ConnProfile{Name: "导入库", Kind: "sqlite", Path: dbPath}
	if _, err := app.ExportProfiles(src, []app.ConnProfile{want}, false); err != nil {
		t.Fatal(err)
	}

	added, err := importProfilesFrom(src, a.store.List())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.treeData) == 0 {
		t.Fatal("parsing profiles must not clear the object tree")
	}
	if len(added) != 1 {
		t.Fatalf("added = %d", len(added))
	}

	if n := a.applyImportedProfiles(added); n != 1 {
		t.Fatalf("applied = %d", n)
	}
	if len(a.treeData) != 0 {
		t.Fatal("applying imports must reload the tree")
	}
	if got := len(a.store.List()); got != 1 {
		t.Fatalf("store conns = %d", got)
	}
}

func TestImportThemeWorkDoesNotTouchUI(t *testing.T) {
	a := newConsoleRoot(t)
	before := a.themePrefs
	src := filepath.Join(t.TempDir(), "theme.json")
	want := app.ThemePrefs{Mode: "light", Accent: "#7c3aed", Density: "roomy", Radius: 4, FontSize: 15}
	if err := app.ExportThemePrefs(src, want); err != nil {
		t.Fatal(err)
	}

	prefs, warn, err := importThemeFile(src)
	if err != nil || len(warn) != 0 {
		t.Fatalf("import: warn=%v err=%v", warn, err)
	}
	if a.themePrefs != before {
		t.Fatal("parsing a theme must not apply it")
	}

	a.applyImportedTheme(prefs, warn)
	if a.themePrefs != prefs {
		t.Fatalf("applied theme = %+v, want %+v", a.themePrefs, prefs)
	}
}
