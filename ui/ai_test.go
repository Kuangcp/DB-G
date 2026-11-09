package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/app"
	"db-g/engine"
)

type fakeAIProvider struct {
	req    ai.Request
	events []ai.Event
	err    error
}

func (f *fakeAIProvider) Models(context.Context) ([]string, error) { return []string{"fake"}, nil }

func (f *fakeAIProvider) Stream(_ context.Context, req ai.Request) (<-chan ai.Event, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan ai.Event, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func TestAIChatSendsContextAndStreams(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "AI")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.catalog.finish(prof.ID, []string{"users"}, nil)
	a.catalog.finishColumns(prof.ID, "users", []engine.Column{
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "name", Type: "TEXT"},
	})
	a.setActiveText("select * from users where id = 1")

	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT name FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiTestProvider = p
	a.aiPrefs.Model = "fake"
	a.aiChat.open = true
	a.aiChat.input = "查 users 的 name"

	a.sendAI()
	if len(a.aiChat.messages) != 2 {
		t.Fatalf("messages: %+v", a.aiChat.messages)
	}
	if got := a.aiChat.messages[1].text; !strings.Contains(got, "SELECT name") {
		t.Fatalf("stream text: %q", got)
	}
	if !a.aiChat.messages[1].done || a.aiChat.streaming {
		t.Fatalf("stream state: done=%v streaming=%v", a.aiChat.messages[1].done, a.aiChat.streaming)
	}
	user := p.req.Messages[1].Content
	for _, want := range []string{"users", "select * from users", "id"} {
		if !strings.Contains(user, want) {
			t.Fatalf("context missing %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "最近查询") {
		t.Fatal("history must not be sent by default")
	}
}

func TestAIHistoryToggle(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	a.history = app.OpenHistory(t.TempDir())
	if err := a.history.Add(app.HistoryEntry{ConnID: prof.ID, SQL: "select 42", OK: true}); err != nil {
		t.Fatal(err)
	}
	if snap := a.aiSnapshot(); len(snap.History) != 0 {
		t.Fatalf("history must default off: %+v", snap.History)
	}
	a.aiPrefs.AllowHistory = true
	if snap := a.aiSnapshot(); len(snap.History) != 1 || snap.History[0].SQL != "select 42" {
		t.Fatalf("history toggle: %+v", snap.History)
	}
}

func TestAIStopIgnoresLateEvents(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiChat.streaming = true
	a.aiChat.seq = 1
	a.aiChat.messages = []aiChatMessage{{role: "assistant"}}
	canceled := false
	a.aiChat.cancel = func() { canceled = true }
	a.stopAI()
	if !canceled || a.aiChat.streaming {
		t.Fatalf("stop: canceled=%v streaming=%v", canceled, a.aiChat.streaming)
	}
	if msg := a.aiChat.messages[0]; !msg.done || msg.err != "已停止" {
		t.Fatalf("message: %+v", msg)
	}
	a.applyAIEvent(1, ai.Event{Kind: ai.EventText, Text: "late"})
	if a.aiChat.messages[0].text != "" {
		t.Fatal("late events must be ignored")
	}
}

func TestAIInsertSQL(t *testing.T) {
	a := newConsoleRoot(t)
	if !a.insertAISQL("说明\n```sql\nSELECT 1;\n```") {
		t.Fatal("extract SQL")
	}
	if got := a.code.Text(); !strings.Contains(got, "SELECT 1;") {
		t.Fatalf("editor: %q", got)
	}
}

func TestAIPanelAndSettings(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("AI 助手"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.aiChat.open || !tt.HasText("发送") {
		t.Fatalf("AI panel missing: open=%v texts=%q", a.aiChat.open, tt.Texts())
	}
	tt2 := openSettingsSection(t, a, "AI")
	if !tt2.HasText("服务地址") || !tt2.HasText("模型") || !tt2.HasText("API Key（本地 Ollama 可留空）") {
		t.Fatalf("AI settings missing: %q", tt2.Texts())
	}
}

func TestAIChatPersistsPerWorkspace(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiPrefsPath = filepath.Join(t.TempDir(), "ai.json")
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "AI")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.aiChat.input = "hi"
	a.sendAI()

	path := a.aiChatFile()
	if path == "" {
		t.Fatal("workspace chat path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("conversation not written: %v", err)
	}
	a.aiChat.messages = nil
	a.loadAIChat()
	if len(a.aiChat.messages) != 2 || a.aiChat.messages[1].text != "ok" {
		t.Fatalf("reloaded messages: %+v", a.aiChat.messages)
	}
}
