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
	if !tt.HasText("标签") {
		t.Fatalf("tag editor missing: %q", tt.Texts())
	}
	a.connFormName = "带标签的库"
	a.connFormPath = path
	a.connFormTags = []connTagDraft{
		{label: "  生产  ", color: "red"},
		{label: "只读", color: "violet"},
	}
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
	if len(a.connFormTags) != 2 {
		t.Fatalf("tags = %+v", a.connFormTags)
	}
	if got := a.connFormTags[0]; got.label != "生产" || got.color != "red" {
		t.Fatalf("tag 0 = %+v", got)
	}
	if a.connFormTagInput != "" || a.connFormTagColor != defaultTagColor(0) {
		t.Fatalf("input state = %q %q", a.connFormTagInput, a.connFormTagColor)
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

func TestAddConnFormTagLimits(t *testing.T) {
	a := newTestRoot()
	a.resetConnTagForm()
	if !a.addConnFormTag("  生产  ", "red") {
		t.Fatal("first tag must be added")
	}
	if a.connFormTags[0].label != "生产" {
		t.Fatalf("label = %q", a.connFormTags[0].label)
	}
	if a.addConnFormTag("生产", "blue") {
		t.Fatal("duplicate tag must be rejected")
	}
	a.addConnFormTag("只读", "")
	a.addConnFormTag("灰度", "green")
	if got := a.connFormTags[1].color; got != a.connFormTagColor {
		t.Fatalf("empty color must use current swatch: %q", got)
	}
	if a.addConnFormTag("第四个", "red") {
		t.Fatal("fourth tag must be rejected")
	}
	if len(a.connFormTags) != app.MaxConnTags {
		t.Fatalf("tags = %+v", a.connFormTags)
	}
	a.removeConnFormTag("只读")
	if len(a.connFormTags) != 2 || a.connFormTags[1].label != "灰度" {
		t.Fatalf("after remove: %+v", a.connFormTags)
	}
}

func TestTagSuggestionsExcludeSelectedAndMatchFuzzy(t *testing.T) {
	a := newSidebarRoot(t)
	path := seedConnFile(t)
	p := app.ConnProfile{
		Name: "已有标签", Kind: "sqlite", Path: path,
		Tags: []app.ConnTag{
			{Label: "生产", Color: "red"},
			{Label: "灰度", Color: "green"},
			{Label: "只读", Color: "gray"},
		},
	}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	a.resetConnTagForm()
	a.addConnFormTag("生产", "red")
	a.connFormTagInput = "灰"
	got := a.tagSuggestions()
	if len(got) != 1 || got[0].Label != "灰度" || got[0].Color != "green" {
		t.Fatalf("fuzzy matches = %+v", got)
	}
	a.connFormTagInput = ""
	if got := a.tagSuggestions(); len(got) != 2 {
		t.Fatalf("all matches = %+v", got)
	}
}

func TestTagInputChoosesExistingSuggestion(t *testing.T) {
	a := newSidebarRoot(t)
	path := seedConnFile(t)
	p := app.ConnProfile{
		Name: "已有标签", Kind: "sqlite", Path: path,
		Tags: []app.ConnTag{{Label: "灰度", Color: "green"}},
	}
	if err := a.store.Add(p); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(rootView(a), 1400, 900)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("添加标签"); err != nil {
		t.Fatal(err)
	}
	tt.Type("灰")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.connFormTags) != 1 {
		t.Fatalf("tags = %+v", a.connFormTags)
	}
	if got := a.connFormTags[0]; got.label != "灰度" || got.color != "green" {
		t.Fatalf("chosen tag = %+v", got)
	}
	if a.connFormTagInput != "" {
		t.Fatalf("input must clear after choosing: %q", a.connFormTagInput)
	}
}

func TestTagChipRemove(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 900)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("添加标签"); err != nil {
		t.Fatal(err)
	}
	tt.Type("生产")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.connFormTags) != 1 {
		t.Fatalf("tags = %+v", a.connFormTags)
	}
	if err := tt.Click("移除标签 生产"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.connFormTags) != 0 {
		t.Fatalf("tag not removed: %+v", a.connFormTags)
	}
}

func TestTagInputEnterAddsTag(t *testing.T) {
	a := newSidebarRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 900)
	tt.Frame()
	if err := tt.Click("新建连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("添加标签"); err != nil {
		t.Fatal(err)
	}
	tt.Type("生产")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.connFormTags) != 1 || a.connFormTags[0].label != "生产" {
		t.Fatalf("tags = %+v", a.connFormTags)
	}
	if a.connFormTagInput != "" {
		t.Fatalf("input must clear after adding: %q", a.connFormTagInput)
	}
	if !tt.HasText("生产") {
		t.Fatalf("chip missing: %q", tt.Texts())
	}
}
