package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
)

func TestAILintModalShowsRisksAndIssues(t *testing.T) {
	a, _ := newSelfCheckRoot(t)
	if !a.insertAISQL("```sql\ndelete from missing_t where 1=1;\n```") {
		t.Fatal("insert failed")
	}
	rep := a.ai.Chat.lint
	if len(rep.Risks) == 0 || len(rep.Issues) == 0 {
		t.Fatalf("expected both risk and issue: %+v", rep)
	}
	a.ai.LintReport = rep
	a.ai.LintOpen = true
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	for _, want := range []string{"SQL 自检", "检查的 SQL", "missing_t"} {
		if !tt.HasText(want) {
			t.Fatalf("lint modal missing %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("复制报告"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); !strings.Contains(got, "missing_t") {
		t.Fatalf("report clipboard: %q", got)
	}
}

func TestAITaskModelsAndUsage(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "usage")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	a.ai.Prefs.Model = "chat-model"
	a.ai.Prefs.CompletionModel = "completion-model"
	a.ai.Prefs.FixModel = "fix-model"
	a.ai.Prefs.ChatTemperature = 0.7
	a.ai.Prefs.ChatMaxTokens = 2222
	a.ai.Prefs.CompletionMaxTokens = 111
	a.ai.Prefs.FixMaxTokens = 333
	a.ai.Prefs.RewriteMaxTokens = 444
	if got := a.aiTaskModel("completion"); got != "completion-model" {
		t.Fatalf("completion model = %q", got)
	}
	if got := a.aiTaskModel("fix"); got != "fix-model" {
		t.Fatalf("fix model = %q", got)
	}
	if got := a.aiTaskModel("chat"); got != "chat-model" {
		t.Fatalf("chat model = %q", got)
	}
	if a.aiTaskMaxTokens("completion") != 111 || a.aiTaskMaxTokens("fix") != 333 ||
		a.aiTaskMaxTokens("rewrite") != 444 || a.aiTaskMaxTokens("chat") != 2222 {
		t.Fatal("task max tokens mismatch")
	}

	a.ai.Prefs.CompletionModel, a.ai.Prefs.FixModel = "", ""
	if got := a.aiTaskModel("completion"); got != "chat-model" {
		t.Fatalf("empty completion model must inherit chat: %q", got)
	}

	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventUsage, Usage: &ai.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}},
		{Kind: ai.EventDone},
	}}
	a.ai.TestProvider = p
	a.ai.Chat.input = "hi"
	a.sendAI()
	if p.req.Model != "chat-model" || p.req.Temperature != 0.7 || p.req.MaxTokens != 2222 {
		t.Fatalf("chat request: %+v", p.req)
	}
	if a.ai.Chat.usage.PromptTokens != 3 || a.ai.Chat.totalUsage.TotalTokens != 7 {
		t.Fatalf("usage: last=%+v total=%+v", a.ai.Chat.usage, a.ai.Chat.totalUsage)
	}
	// 同一会话再来一次，累计只加一次。
	p.events = []ai.Event{
		{Kind: ai.EventUsage, Usage: &ai.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
		{Kind: ai.EventDone},
	}
	a.ai.Chat.input = "again"
	a.sendAI()
	if a.ai.Chat.totalUsage.TotalTokens != 10 {
		t.Fatalf("session usage: %+v", a.ai.Chat.totalUsage)
	}
}
