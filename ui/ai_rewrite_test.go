package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
)

func TestCtrlKOpensRewriteForSelection(t *testing.T) {
	const sql = "select * from users where id = 1"
	a, tt := focusEditor(t, sql)
	a.setSelection(0, len([]rune(sql)))
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()

	if !a.aiRewrite.open {
		t.Fatal("Ctrl+K should open the rewrite modal")
	}
	if a.aiRewrite.target != sql {
		t.Fatalf("target: %q", a.aiRewrite.target)
	}
	if got := a.code.Text(); got != sql {
		t.Fatalf("Ctrl+K must not cut to line end: %q", got)
	}
	if !tt.HasText("AI 改写 SQL") {
		t.Fatalf("modal missing: %q", tt.Texts())
	}
}

func TestAIRewriteReplacesSelection(t *testing.T) {
	const sql = "select * from users where id = 1"
	a, tt := focusEditor(t, sql)
	a.setSelection(0, len([]rune(sql)))
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()

	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT id FROM users WHERE id = 1\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiTestProvider = p
	a.aiPrefs.Model = "fake"
	a.aiRewrite.instruction = "只查 id"
	a.startAIRewrite()

	if a.aiRewrite.loading || a.aiRewrite.err != "" || a.aiRewrite.diff == nil {
		t.Fatalf("rewrite state: %+v", a.aiRewrite)
	}
	if got := a.aiRewrite.result; got != "SELECT id FROM users WHERE id = 1" {
		t.Fatalf("result: %q", got)
	}
	body := p.req.Messages[1].Content
	for _, want := range []string{"待改写 SQL", "只查 id", sql} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q:\n%s", want, body)
		}
	}
	a.applyAIRewrite(false)
	if got := a.code.Text(); got != "SELECT id FROM users WHERE id = 1" {
		t.Fatalf("editor: %q", got)
	}
	if a.aiRewrite.open {
		t.Fatal("modal should close after apply")
	}
}

func TestAIRewriteAppendsAfterSelection(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;")
	a.setSelection(0, len([]rune("SELECT 1;")))
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT 2;\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.aiRewrite.instruction = "再来一条"
	a.startAIRewrite()
	a.applyAIRewrite(true)
	if got := a.code.Text(); got != "SELECT 1;\nSELECT 2;" {
		t.Fatalf("append: %q", got)
	}
}

func TestAIRewriteRejectsStaleTarget(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;")
	a.setSelection(0, len([]rune("SELECT 1;")))
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT 2;\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.aiRewrite.instruction = "改成 2"
	a.startAIRewrite()
	a.setActiveText("SELECT 9;")
	a.applyAIRewrite(false)
	if a.aiRewrite.err == "" {
		t.Fatal("stale target must be rejected")
	}
	if got := a.code.Text(); got != "SELECT 9;" {
		t.Fatalf("editor must stay unchanged: %q", got)
	}
}

// 没有选区时用光标所在语句。
func TestAIRewriteFallsBackToStatement(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1;\nSELECT 2;")
	a.setSelection(15, 15)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !a.aiRewrite.open || !strings.Contains(a.aiRewrite.target, "SELECT 2") {
		t.Fatalf("rewrite state: %+v", a.aiRewrite)
	}
}
