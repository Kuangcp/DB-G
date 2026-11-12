package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFontPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fonts.json")
	if got, warn := LoadFontPrefs(path); warn != "" || got != DefaultFontPrefs() {
		t.Fatalf("缺文件应用默认值: %+v %q", got, warn)
	}
	want := FontPrefs{
		Editor:  FontSpec{Family: "JetBrains Mono", Size: 16},
		Result:  FontSpec{Family: "monospace", Size: 12},
		TabSize: 14.5,
		UI:      "Noto Sans CJK SC",
	}
	if err := SaveFontPrefs(path, want); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadFontPrefs(path)
	if warn != "" || got != want {
		t.Fatalf("%+v %q", got, warn)
	}
}

func TestFontPrefsClampsAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fonts.json")
	if err := os.WriteFile(path, []byte(`{"editor":{"size":99},"result":{"size":4},"ui":"  IBM Plex Sans  "}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadFontPrefs(path)
	if warn != "" {
		t.Fatalf("合法 JSON 不该告警: %q", warn)
	}
	if got.Editor.Size != 13 {
		t.Fatalf("越界字号应回默认: %+v", got)
	}
	if got.Result.Size != 0 {
		t.Fatalf("结果区非法字号应回到跟随主题: %+v", got)
	}
	if got.UI != "IBM Plex Sans" {
		t.Fatalf("字体族应去空白: %q", got.UI)
	}
	if got.TabSize != DefaultTabFontSize {
		t.Fatalf("缺失的控制台标签字号应用默认值: %+v", got)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, warn := LoadFontPrefs(path); warn == "" {
		t.Fatal("损坏文件应告警")
	}
}

func TestFontPrefsMigratesEditorJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "editor.json")
	if err := os.WriteFile(legacy, []byte(`{"fontSize":15,"fontFamily":"Sarasa Mono SC"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fonts.json")
	got, warn := LoadFontPrefs(path)
	if warn != "" {
		t.Fatal(warn)
	}
	if got.Editor.Size != 15 || got.Editor.Family != "Sarasa Mono SC" {
		t.Fatalf("旧 editor.json 没迁移: %+v", got)
	}
	if got.Result != (FontSpec{}) || got.UI != "" {
		t.Fatalf("其他区域应为默认: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("迁移后应写出 fonts.json: %v", err)
	}
}
