package ui

import (
	"strings"
	"testing"

	"db-g/ai"
)

func TestAICompletionRequestsAndStoresSuggestion(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "补全")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.catalog.finish(prof.ID, []string{"users"}, nil)
	text := "select * from us"
	a.setActiveText(text)
	a.setSelection(len(text), len(text))
	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nFROM users\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiTestProvider = p
	a.aiPrefs.Model = "fake"

	a.requestAICompletion()
	if got := a.aiCompletion.suggestion; got != "FROM users" {
		t.Fatalf("suggestion: %q", got)
	}
	if a.aiCompletion.loading {
		t.Fatal("completion should be done")
	}
	body := p.req.Messages[1].Content
	for _, want := range []string{"select * from us", "<CURSOR>", "users"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q:\n%s", want, body)
		}
	}
}

func TestAICompletionIgnoresStaleVersion(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "FROM users"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.setActiveText("select * from us")
	a.setSelection(len("select * from us"), len("select * from us"))
	a.requestAICompletion()
	// 请求完成后又改了正文：旧建议不应弹层（状态里仍保留，但 show 会丢弃）。
	a.setActiveText("select * from users where id = 1")
	a.showAICompletion(a.aiCompletion.seq)
	if !strings.Contains(a.code.Text(), "where id = 1") {
		t.Fatalf("editor text changed unexpectedly: %q", a.code.Text())
	}
}
