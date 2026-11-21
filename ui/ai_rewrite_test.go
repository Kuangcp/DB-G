package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/app"
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
	if !tt.HasText("AI 改写 SQL") || !tt.HasText("Ctrl+Enter 发送指令") {
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
	a.aiPrefsPath = filepath.Join(t.TempDir(), "ai.json")
	a.history = app.OpenHistory(t.TempDir())
	a.aiRewrite.instruction = "只查 id"
	a.startAIRewrite()

	if a.aiRewrite.loading || a.aiRewrite.err != "" || a.aiRewrite.diff == nil {
		t.Fatalf("rewrite state: %+v", a.aiRewrite)
	}
	if got := a.aiRewrite.result; got != "SELECT id FROM users WHERE id = 1" {
		t.Fatalf("result: %q", got)
	}
	body := p.req.Messages[1].Content
	for _, want := range []string{"SQL to Rewrite", "只查 id", sql} {
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
	// 执行历史只记真实执行；这次改写不能混进去。
	if entries := a.history.List(a.activeConnID(), 10); len(entries) != 0 {
		t.Fatalf("execution history must stay clean: %+v", entries)
	}
	// Ctrl+K 的对话进 AI 助手会话历史。
	store, warn := app.LoadAIConversations(a.aiChatFile())
	if warn != "" {
		t.Fatal(warn)
	}
	if len(store.Conversations) != 1 {
		t.Fatalf("AI conversations: %+v", store.Conversations)
	}
	conv := store.Conversations[0]
	if !strings.HasPrefix(conv.Title, "改写：只查 id") {
		t.Fatalf("title: %q", conv.Title)
	}
	if len(conv.Messages) != 2 || !strings.Contains(conv.Messages[1].Text, "SELECT id FROM users") {
		t.Fatalf("messages: %+v", conv.Messages)
	}

	// AI 助手的「会话历史」里能看到这次改写。
	a.aiChat.open = true
	a.aiChat.showHistory = true
	a.loadAIChat()
	tt.Frame()
	if !tt.HasText("改写：只查 id") {
		t.Fatalf("AI history missing rewrite: %q", tt.Texts())
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
