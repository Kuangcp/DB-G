package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditorPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editor.json")
	if got, warn := LoadEditorPrefs(path); warn != "" || got.FontSize != 13 {
		t.Fatalf("缺文件应用默认值: %+v %q", got, warn)
	}
	want := EditorPrefs{FontSize: 16, FontFamily: "JetBrains Mono"}
	if err := SaveEditorPrefs(path, want); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadEditorPrefs(path)
	if warn != "" || got != want {
		t.Fatalf("%+v %q", got, warn)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("权限 %v", info.Mode().Perm())
	}
}

func TestEditorPrefsClampsAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editor.json")
	if err := os.WriteFile(path, []byte(`{"fontSize": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, warn := LoadEditorPrefs(path); warn != "" || got.FontSize != 13 {
		t.Fatalf("越界字号应回默认: %+v %q", got, warn)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, warn := LoadEditorPrefs(path); warn == "" {
		t.Fatal("损坏文件应告警")
	}
}

func TestVersionNotEmpty(t *testing.T) {
	if Version() == "" {
		t.Fatal("版本号不能为空")
	}
}
