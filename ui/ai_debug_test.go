package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
)

// 每次真正发往模型的请求都应记录在内存里，便于核对上下文。
func TestAIRecordsRequestsForDebug(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT 1;\n```"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.ai.Chat.input = "查一下"
	a.sendAI()

	if len(a.ai.Debug.entries) != 1 {
		t.Fatalf("debug entries: %+v", a.ai.Debug.entries)
	}
	e := a.ai.Debug.entries[0]
	if e.Kind != "对话" || e.Model != "fake" || e.Tokens == 0 {
		t.Fatalf("debug entry: %+v", e)
	}
	if !strings.Contains(a.ai.Debug.body, "查一下") || !strings.Contains(a.ai.Debug.body, string(ai.RoleSystem)) {
		t.Fatalf("debug body should contain both messages:\n%s", a.ai.Debug.body)
	}

	a.openAIDebug()
	if !a.ai.Debug.open {
		t.Fatal("debug modal should open")
	}
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if !tt.HasText("最近 AI 请求") || !tt.HasText("复制全文") {
		t.Fatalf("debug modal missing: %q", tt.Texts())
	}
}

// 修复、补全、改写也要进调试列表，不能只有对话。
func TestAIRecordsAllFeatures(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT 2;\n```"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"

	a.con.SetOutcome(nil, "boom")
	a.tabs.LastExecSQL = "select 1"
	a.fixErrorSQL()

	a.setActiveText("select * from us")
	a.setSelection(len("select * from us"), len("select * from us"))
	a.requestAICompletion()

	a.ai.Rewrite = aiRewriteState{open: true, target: "select 1", start: 0, end: 8, version: a.code.Version()}
	a.ai.Rewrite.instruction = "改成 2"
	a.startAIRewrite()

	kinds := make(map[string]bool)
	for _, e := range a.ai.Debug.entries {
		kinds[e.Kind] = true
	}
	for _, want := range []string{"修复", "补全", "改写"} {
		if !kinds[want] {
			t.Fatalf("missing %q in debug entries: %+v", want, a.ai.Debug.entries)
		}
	}
}
