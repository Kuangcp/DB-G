package ui

import (
	"context"
	"fmt"
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
	calls  int
}

func (f *fakeAIProvider) Models(context.Context) ([]string, error) { return []string{"fake"}, nil }

func (f *fakeAIProvider) Stream(_ context.Context, req ai.Request) (<-chan ai.Event, error) {
	f.calls++
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

func TestAIPanelWidthSurvivesHideShow(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.aiChat.open = true
	tt.Frame()
	first, ok := tt.Find("AI 面板")
	if !ok || first.W < 200 {
		t.Fatalf("AI panel width: %v ok=%v", first.W, ok)
	}
	a.aiChat.open = false
	tt.Frame()
	a.aiChat.open = true
	tt.Frame()
	second, ok := tt.Find("AI 面板")
	if !ok || second.W < 200 {
		t.Fatalf("AI panel reopened width: %v ok=%v", second.W, ok)
	}
	if d := first.W - second.W; d < -2 || d > 2 {
		t.Fatalf("AI panel width changed: %v -> %v", first.W, second.W)
	}
}

func TestAIChatConversationHistorySwitch(t *testing.T) {
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
	a.aiChat.open = true
	a.loadAIChat()

	a.aiChat.input = "first question"
	a.sendAI()
	firstID := a.aiChat.convID
	if firstID == "" || len(a.aiChat.messages) != 2 {
		t.Fatalf("first conversation: id=%q messages=%+v", firstID, a.aiChat.messages)
	}

	a.resetAIChat()
	secondID := a.aiChat.convID
	if secondID == "" || secondID == firstID || len(a.aiChat.messages) != 0 {
		t.Fatalf("new conversation: id=%q messages=%+v", secondID, a.aiChat.messages)
	}
	a.aiChat.input = "second question"
	a.sendAI()
	if len(a.aiChat.store.Conversations) != 2 {
		t.Fatalf("store conversations: %+v", a.aiChat.store.Conversations)
	}
	a.aiChat.showHistory = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("会话历史") || !tt.HasText("first question") || !tt.HasText("second question") {
		t.Fatalf("history list: %q", tt.Texts())
	}
	if row, ok := tt.Find("first question"); !ok || row.H < 10 {
		t.Fatalf("history row must render text, not a thin strip: %+v ok=%v", row, ok)
	}
	a.aiChat.showHistory = false

	before := map[string]int64{}
	for _, conv := range a.aiChat.store.Conversations {
		before[conv.ID] = conv.UpdatedAt
	}
	a.selectAIConversation(firstID, true)
	if len(a.aiChat.messages) != 2 || a.aiChat.messages[0].text != "first question" {
		t.Fatalf("switched messages: %+v", a.aiChat.messages)
	}
	for _, conv := range a.aiChat.store.Conversations {
		if conv.UpdatedAt != before[conv.ID] {
			t.Fatalf("switching must not reorder history: %s %d -> %d", conv.ID, before[conv.ID], conv.UpdatedAt)
		}
	}
	store, warn := app.LoadAIConversations(a.aiChatFile())
	if warn != "" {
		t.Fatal(warn)
	}
	if store.ActiveID != firstID || len(store.Conversations) != 2 {
		t.Fatalf("saved store: %+v", store)
	}
}

func TestAITitleGenerationIsOptIn(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "AI")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventDone},
	}}
	a.aiTestProvider = p
	a.aiPrefs.Model = "fake"
	a.aiChat.open = true
	a.loadAIChat()
	a.aiChat.input = "first"
	a.sendAI()
	if p.calls != 1 {
		t.Fatalf("title generation must be off by default: calls=%d", p.calls)
	}
	a.aiPrefs.AITitle = true
	a.resetAIChat()
	a.aiChat.input = "second"
	a.sendAI()
	if p.calls != 3 {
		t.Fatalf("title generation should add one call: calls=%d", p.calls)
	}
}

func TestAIHistorySearchFiltersTitles(t *testing.T) {
	a := newConsoleRoot(t)
	a.aiChat.store = app.AIConversationStore{Conversations: []app.AIConversation{
		{ID: "a", Title: "alpha report", UpdatedAt: 2},
		{ID: "b", Title: "beta query", UpdatedAt: 1},
	}}
	a.aiChat.convID = "b"
	a.aiChat.open = true
	a.aiChat.showHistory = true
	a.aiChat.historyQuery = "beta"
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("beta query") || tt.HasText("alpha report") {
		t.Fatalf("search filter: %q", tt.Texts())
	}
}

func TestAIHistoryScrollPagination(t *testing.T) {
	a := newConsoleRoot(t)
	for i := 0; i < 30; i++ {
		a.aiChat.store.Conversations = append(a.aiChat.store.Conversations, app.AIConversation{
			ID: fmt.Sprintf("c%02d", i), Title: fmt.Sprintf("conv %02d", i), UpdatedAt: int64(100 - i),
		})
	}
	a.aiChat.open = true
	a.aiChat.showHistory = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("继续向下滚动加载更多…") {
		t.Fatalf("first page should show load-more hint: %q", tt.Texts())
	}
	a.aiChat.historyScroll.Y = a.aiChat.historyScroll.MaxY
	tt.Frame()
	if a.aiChat.historyLimit != 30 {
		t.Fatalf("scroll to end should load all remaining rows: limit=%d", a.aiChat.historyLimit)
	}
}
