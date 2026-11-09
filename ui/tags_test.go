package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestResultFooterShowsDatabaseVersionAndTags(t *testing.T) {
	a := newConsoleRoot(t)
	path := filepath.Join(t.TempDir(), "footer.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p := app.ConnProfile{
		Name: "底栏库", Kind: "sqlite", Path: path,
		Tags: []app.ConnTag{
			{Label: "生产", Color: "red"},
			{Label: "只读", Color: "blue"},
		},
	}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	p = a.store.List()[0]
	if err := a.openConn(p); err != nil {
		t.Fatal(err)
	}
	info, ok := a.serverInfo[p.ID]
	if !ok || info.Version == "" {
		t.Fatalf("server info = %+v", info)
	}
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	for _, want := range []string{"数据库", "footer.db", "版本", info.Version, "生产", "只读"} {
		if !tt.HasText(want) {
			t.Fatalf("footer missing %q in %q", want, tt.Texts())
		}
	}
}

func TestConnFormSavesTags(t *testing.T) {
	a := newSidebarRoot(t)
	path := seedConnFile(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("标签（最多 3 个）") {
		t.Fatalf("tag editor missing: %q", tt.Texts())
	}
	a.connFormName = "带标签的库"
	a.connFormPath = path
	a.connFormTags[0] = connTagDraft{label: "  生产  ", color: "red"}
	a.connFormTags[1] = connTagDraft{label: "只读", color: "violet"}
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	list := a.store.List()
	if len(list) != 1 {
		t.Fatalf("store: %+v", list)
	}
	tags := list[0].Tags
	if len(tags) != 2 || tags[0].Label != "生产" || tags[0].Color != "red" ||
		tags[1].Label != "只读" || tags[1].Color != "violet" {
		t.Fatalf("saved tags: %+v", tags)
	}
}

func TestConnFormLoadsTagsForEdit(t *testing.T) {
	a := newSidebarRoot(t)
	path := seedConnFile(t)
	p := app.ConnProfile{
		Name: "带标签", Kind: "sqlite", Path: path,
		Tags: []app.ConnTag{{Label: "生产", Color: "red"}, {Label: "只读", Color: "green"}},
	}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	p = a.store.List()[0]
	a.setConnTagForm(p.Tags)
	if got := a.connFormTags[0]; got.label != "生产" || got.color != "red" {
		t.Fatalf("slot 0 = %+v", got)
	}
	if got := a.connFormTags[2]; got.label != "" || got.color != defaultTagColor(2) {
		t.Fatalf("empty slot = %+v", got)
	}
}

func TestNextTagColorCyclesPalette(t *testing.T) {
	key := app.DefaultConnTagColor
	seen := map[string]bool{key: true}
	for range len(tagColorSpecs) - 1 {
		key = nextTagColor(key)
		if seen[key] {
			t.Fatalf("cycle repeated %q early", key)
		}
		seen[key] = true
	}
	if got := nextTagColor(key); got != app.DefaultConnTagColor {
		t.Fatalf("cycle end = %q, want %q", got, app.DefaultConnTagColor)
	}
	if tagColorLabel("nope") != tagColorLabel(app.DefaultConnTagColor) {
		t.Fatal("unknown color must fall back to default")
	}
}
