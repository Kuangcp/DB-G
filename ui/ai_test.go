package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/ai/plan"
	"db-g/app"
	"db-g/engine"
)

type fakeAIProvider struct {
	req      ai.Request
	events   []ai.Event
	err      error
	models   []string
	modelErr error
	calls    int
}

func (f *fakeAIProvider) Models(context.Context) ([]string, error) {
	if f.modelErr != nil {
		return nil, f.modelErr
	}
	if f.models != nil {
		return f.models, nil
	}
	return []string{"fake"}, nil
}

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
	a.session.Catalog.finish(prof.ID, []string{"users"}, nil, "")
	a.session.Catalog.finishColumns(prof.ID, "users", []engine.Column{
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "name", Type: "TEXT"},
	}, "")
	a.setActiveText("select * from users where id = 1")

	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT name FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.ai.TestProvider = p
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.ai.Chat.input = "查 users 的 name"

	a.sendAI()
	if len(a.ai.Chat.messages) != 2 {
		t.Fatalf("messages: %+v", a.ai.Chat.messages)
	}
	if got := a.ai.Chat.messages[1].text; !strings.Contains(got, "SELECT name") {
		t.Fatalf("stream text: %q", got)
	}
	if !a.ai.Chat.messages[1].done || a.ai.Chat.streaming {
		t.Fatalf("stream state: done=%v streaming=%v", a.ai.Chat.messages[1].done, a.ai.Chat.streaming)
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
	a.ai.Prefs.AllowHistory = true
	if snap := a.aiSnapshot(); len(snap.History) != 1 || snap.History[0].SQL != "select 42" {
		t.Fatalf("history toggle: %+v", snap.History)
	}
}

func TestAIStopIgnoresLateEvents(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.Chat.streaming = true
	a.ai.Chat.seq = 1
	a.ai.Chat.messages = []aiChatMessage{{role: "assistant"}}
	canceled := false
	a.ai.Chat.cancel = func() { canceled = true }
	a.stopAI()
	if !canceled || a.ai.Chat.streaming {
		t.Fatalf("stop: canceled=%v streaming=%v", canceled, a.ai.Chat.streaming)
	}
	if msg := a.ai.Chat.messages[0]; !msg.done || msg.err != "已停止" {
		t.Fatalf("message: %+v", msg)
	}
	a.applyAIEvent(1, ai.Event{Kind: ai.EventText, Text: "late"})
	if a.ai.Chat.messages[0].text != "" {
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
	if !a.ai.Chat.open || !tt.HasText("发送") {
		t.Fatalf("AI panel missing: open=%v texts=%q", a.ai.Chat.open, tt.Texts())
	}
	tt2 := openSettingsSection(t, a, "AI")
	if !tt2.HasText("服务地址") || !tt2.HasText("模型") || !tt2.HasText("API Key（本地 Ollama 可留空）") {
		t.Fatalf("AI settings missing: %q", tt2.Texts())
	}
}

func TestAIChatPersistsPerWorkspace(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.PrefsPath = filepath.Join(t.TempDir(), "ai.json")
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "AI")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.input = "hi"
	a.sendAI()

	path := a.aiChatFile()
	if path == "" {
		t.Fatal("workspace chat path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("conversation not written: %v", err)
	}
	a.ai.Chat.messages = nil
	a.loadAIChat()
	if len(a.ai.Chat.messages) != 2 || a.ai.Chat.messages[1].text != "ok" {
		t.Fatalf("reloaded messages: %+v", a.ai.Chat.messages)
	}
}

func TestAIPanelWidthSurvivesHideShow(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	a.ai.Chat.open = true
	tt.Frame()
	first, ok := tt.Find("AI 面板")
	if !ok || first.W < 200 {
		t.Fatalf("AI panel width: %v ok=%v", first.W, ok)
	}
	a.ai.Chat.open = false
	tt.Frame()
	a.ai.Chat.open = true
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
	a.ai.PrefsPath = filepath.Join(t.TempDir(), "ai.json")
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "AI")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.loadAIChat()

	a.ai.Chat.input = "first question"
	a.sendAI()
	firstID := a.ai.Chat.convID
	if firstID == "" || len(a.ai.Chat.messages) != 2 {
		t.Fatalf("first conversation: id=%q messages=%+v", firstID, a.ai.Chat.messages)
	}

	a.resetAIChat()
	secondID := a.ai.Chat.convID
	if secondID == "" || secondID == firstID || len(a.ai.Chat.messages) != 0 {
		t.Fatalf("new conversation: id=%q messages=%+v", secondID, a.ai.Chat.messages)
	}
	a.ai.Chat.input = "second question"
	a.sendAI()
	if len(a.ai.Chat.store.Conversations) != 2 {
		t.Fatalf("store conversations: %+v", a.ai.Chat.store.Conversations)
	}
	a.ai.Chat.showHistory = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("会话历史") || !tt.HasText("first question") || !tt.HasText("second question") {
		t.Fatalf("history list: %q", tt.Texts())
	}
	if row, ok := tt.Find("first question"); !ok || row.H < 10 {
		t.Fatalf("history row must render text, not a thin strip: %+v ok=%v", row, ok)
	}
	a.ai.Chat.showHistory = false

	before := map[string]int64{}
	for _, conv := range a.ai.Chat.store.Conversations {
		before[conv.ID] = conv.UpdatedAt
	}
	a.selectAIConversation(firstID, true)
	if len(a.ai.Chat.messages) != 2 || a.ai.Chat.messages[0].text != "first question" {
		t.Fatalf("switched messages: %+v", a.ai.Chat.messages)
	}
	for _, conv := range a.ai.Chat.store.Conversations {
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
	a.ai.TestProvider = p
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.loadAIChat()
	a.ai.Chat.input = "first"
	a.sendAI()
	if p.calls != 1 {
		t.Fatalf("title generation must be off by default: calls=%d", p.calls)
	}
	a.ai.Prefs.AITitle = true
	a.resetAIChat()
	a.ai.Chat.input = "second"
	a.sendAI()
	if p.calls != 3 {
		t.Fatalf("title generation should add one call: calls=%d", p.calls)
	}
}

func TestAIHistorySearchFiltersTitles(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.Chat.store = app.AIConversationStore{Conversations: []app.AIConversation{
		{ID: "a", Title: "alpha report", UpdatedAt: 2},
		{ID: "b", Title: "beta query", UpdatedAt: 1},
	}}
	a.ai.Chat.convID = "b"
	a.ai.Chat.open = true
	a.ai.Chat.showHistory = true
	a.ai.Chat.historyQuery = "beta"
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("beta query") || tt.HasText("alpha report") {
		t.Fatalf("search filter: %q", tt.Texts())
	}
}

func TestAIHistoryScrollPagination(t *testing.T) {
	a := newConsoleRoot(t)
	for i := 0; i < 30; i++ {
		a.ai.Chat.store.Conversations = append(a.ai.Chat.store.Conversations, app.AIConversation{
			ID: fmt.Sprintf("c%02d", i), Title: fmt.Sprintf("conv %02d", i), UpdatedAt: int64(100 - i),
		})
	}
	a.ai.Chat.open = true
	a.ai.Chat.showHistory = true
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("继续向下滚动加载更多…") {
		t.Fatalf("first page should show load-more hint: %q", tt.Texts())
	}
	a.ai.Chat.historyScroll.Y = a.ai.Chat.historyScroll.MaxY
	tt.Frame()
	if a.ai.Chat.historyLimit != 30 {
		t.Fatalf("scroll to end should load all remaining rows: limit=%d", a.ai.Chat.historyLimit)
	}
}

func TestAIChatCtrlEnterSendsFromInput(t *testing.T) {
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
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "ok"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.loadAIChat()
	a.ai.Chat.input = "hello"
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("AI 提问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if a.con.Result() != nil || a.con.Running() {
		t.Fatal("Ctrl+Enter in AI input must not execute the editor")
	}
	if len(a.ai.Chat.messages) != 2 || a.ai.Chat.messages[0].text != "hello" {
		t.Fatalf("Ctrl+Enter should send the AI prompt: %+v", a.ai.Chat.messages)
	}
}

func TestAIChatEnterAddsNewline(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.Chat.open = true
	a.ai.Chat.input = "line one"
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("AI 提问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.ai.Chat.messages) != 0 {
		t.Fatalf("Enter must not send: %+v", a.ai.Chat.messages)
	}
	if !strings.Contains(a.ai.Chat.input, "\n") {
		t.Fatalf("Enter should insert a newline: %q", a.ai.Chat.input)
	}
}

func TestAIChatMessageAlignmentAndCopy(t *testing.T) {
	a := newConsoleRoot(t)
	a.ai.Chat.open = true
	a.ai.Chat.messages = []aiChatMessage{
		{role: "user", text: "hello from me", done: true},
		{role: "assistant", text: "answer here", done: true},
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	user, okUser := tt.Find("hello from me")
	aiMsg, okAI := tt.Find("answer here")
	if !okUser || !okAI {
		t.Fatalf("messages missing: user=%v ai=%v texts=%q", okUser, okAI, tt.Texts())
	}
	if user.X <= aiMsg.X {
		t.Fatalf("user message should align right: user=%+v ai=%+v", user, aiMsg)
	}
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "answer here" {
		t.Fatalf("copy answer: %q", got)
	}
}

// 多 schema 连接必须按 completionScope 的 key 取表/列，否则 AI 上下文为空。
func TestAISnapshotUsesCurrentSchemaKey(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "schema")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.tree.ConnSchema[prof.ID] = "shop"
	key := prof.ID + "|shop"
	a.session.Catalog.finish(key, []string{"orders"}, nil, "mysql")
	a.session.Catalog.finishColumns(key, "orders", []engine.Column{
		{Name: "id", Type: "bigint", PrimaryKey: true},
		{Name: "amount", Type: "int"},
	}, "mysql")
	a.session.Catalog.finish(prof.ID, []string{"wrong_default"}, nil, "mysql")

	snap := a.aiSnapshot()
	if len(snap.Tables) != 1 || snap.Tables[0].Name != "orders" {
		t.Fatalf("tables must come from the current schema: %+v", snap.Tables)
	}
	if snap.Tables[0].Schema != "shop" {
		t.Fatalf("table schema missing: %+v", snap.Tables[0])
	}
	if len(snap.Tables[0].Columns) != 2 {
		t.Fatalf("columns must use the scoped cache: %+v", snap.Tables[0])
	}
}

// 发送前预览必须与实际请求使用同一套预算组装，且不触发模型调用。
func TestAIPreviewShowsBudgetedContext(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "preview")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.session.Catalog.finish(prof.ID, []string{"users"}, nil, "")
	a.setActiveText("select * from users")
	a.ai.Prefs.ContextWindow = 2048
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.input = "查 users 的行数"

	p := &fakeAIProvider{}
	a.ai.TestProvider = p
	a.openAIPreview()
	if p.calls != 0 {
		t.Fatalf("preview must not call the provider: %d", p.calls)
	}
	if !a.ai.Debug.open || a.ai.Debug.sel != -1 || a.ai.Debug.draft == nil {
		t.Fatalf("preview state: open=%v sel=%d draft=%v", a.ai.Debug.open, a.ai.Debug.sel, a.ai.Debug.draft)
	}
	if !strings.Contains(a.ai.Debug.body, "users") || !strings.Contains(a.ai.Debug.body, "查 users 的行数") {
		t.Fatalf("preview body missing context:\n%s", a.ai.Debug.body)
	}
	if !strings.Contains(a.ai.Debug.draft.Note, "token") {
		t.Fatalf("preview note should carry the token budget: %q", a.ai.Debug.draft.Note)
	}

	// 小窗口 + 大 schema/历史：预览报告必须反映裁剪。
	var names []string
	for i := range 16 {
		name := fmt.Sprintf("big_%02d", i)
		names = append(names, name)
		var cols []engine.Column
		for j := range 30 {
			cols = append(cols, engine.Column{Name: fmt.Sprintf("c_%02d_%02d", i, j), Type: "text"})
		}
		a.session.Catalog.finishColumns(prof.ID, name, cols, "")
	}
	a.session.Catalog.finish(prof.ID, names, nil, "")
	a.history = app.OpenHistory(t.TempDir())
	for i := range 12 {
		if err := a.history.Add(app.HistoryEntry{ConnID: prof.ID, SQL: fmt.Sprintf("select %d", i), OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	a.ai.Prefs.AllowHistory = true
	a.ai.Chat.input = "统计 big_00 的数据"
	a.openAIPreview()
	if !strings.Contains(a.ai.Debug.draft.Note, "已裁剪") {
		t.Fatalf("preview note must report trimming: %q", a.ai.Debug.draft.Note)
	}
}

// 第二次提问必须携带前一轮 user/assistant 消息；当前问题仍放在最后。
func TestAIChatSendsDialogue(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "多轮")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.session.Catalog.finish(prof.ID, []string{"users"}, nil, "")
	a.setActiveText("select * from users")

	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "第一答"},
		{Kind: ai.EventDone},
	}}
	a.ai.TestProvider = p
	a.ai.Prefs.Model = "fake"
	a.ai.Chat.open = true
	a.ai.Chat.input = "第一问"
	a.sendAI()

	p.events = []ai.Event{{Kind: ai.EventText, Text: "第二答"}, {Kind: ai.EventDone}}
	a.ai.Chat.input = "第二问"
	a.sendAI()

	msgs := p.req.Messages
	if len(msgs) != 4 {
		t.Fatalf("second request messages = %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != ai.RoleSystem || msgs[1].Role != ai.RoleUser ||
		msgs[2].Role != ai.RoleAssistant || msgs[3].Role != ai.RoleUser {
		t.Fatalf("roles: %+v", msgs)
	}
	if msgs[1].Content != "第一问" || msgs[2].Content != "第一答" {
		t.Fatalf("dialogue not preserved: %+v", msgs[1:3])
	}
	if !strings.Contains(msgs[3].Content, "第二问") || !strings.Contains(msgs[3].Content, "users") {
		t.Fatalf("current turn missing context:\n%s", msgs[3].Content)
	}
	if strings.Contains(strings.Join([]string{msgs[1].Content, msgs[2].Content}, "\n"), "第二问") {
		t.Fatal("current question must not appear in history")
	}

	// 预览也必须带同一份对话记忆。
	a.ai.Chat.input = "第三问"
	a.openAIPreview()
	if !strings.Contains(a.ai.Debug.body, "第一问") || !strings.Contains(a.ai.Debug.body, "第一答") {
		t.Fatalf("preview must carry dialogue:\n%s", a.ai.Debug.body)
	}
}

func TestAIDialogueKeepsRecentMessages(t *testing.T) {
	a := newConsoleRoot(t)
	for i := range 12 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		a.ai.Chat.messages = append(a.ai.Chat.messages, aiChatMessage{
			role: role, text: fmt.Sprintf("msg-%02d", i), done: true,
		})
	}
	// 未完成/空消息不进入上下文。
	a.ai.Chat.messages = append(a.ai.Chat.messages, aiChatMessage{role: "assistant", text: "", done: false})
	got := a.aiDialogue()
	if len(got) != maxAIDialogueMessages {
		t.Fatalf("dialogue length = %d", len(got))
	}
	if got[0].Content != "msg-04" || got[len(got)-1].Content != "msg-11" {
		t.Fatalf("dialogue window: %+v", got)
	}
	summary, _ := a.aiDialogueParts()
	if !strings.Contains(summary, "msg-00") || !strings.Contains(summary, "省略") {
		t.Fatalf("older dialogue summary: %q", summary)
	}
}

func TestAIResultContextShapeAndSamples(t *testing.T) {
	a := newTestRoot()
	a.tabs.LastExecSQL = "select id, name from users"
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery:       true,
		Columns:       []string{"id", "name"},
		ColumnTypes:   []string{"INTEGER", "TEXT"},
		Rows:          [][]any{{int64(1), "alice"}, {int64(2), nil}},
		HasMore:       true,
		ExecDuration:  12 * time.Millisecond,
		FetchDuration: 3 * time.Millisecond,
	}, "")
	rc := a.aiResultContext()
	if rc == nil || rc.RowCount != 2 || len(rc.Columns) != 2 || !rc.HasMore {
		t.Fatalf("result context: %+v", rc)
	}
	if rc.Columns[1].Type != "TEXT" || rc.ExecMillis != 12 || rc.FetchMillis != 3 {
		t.Fatalf("result details: %+v", rc)
	}
	if len(rc.Sample) != 0 {
		t.Fatalf("samples must stay off by default: %+v", rc.Sample)
	}
	a.ai.Prefs.AllowSampleRows = true
	rc = a.aiResultContext()
	if len(rc.Sample) != 2 || rc.Sample[0][1] != "alice" || rc.Sample[1][1] != "NULL" {
		t.Fatalf("samples: %+v", rc.Sample)
	}
}

func TestAIPlanContextOnlyWhenOpen(t *testing.T) {
	a := newTestRoot()
	a.ai.Plan = planState{open: false, nodes: []plan.PlanNode{{Op: "Seq Scan"}}}
	if got := a.aiPlanContext(); got != nil {
		t.Fatalf("closed plan must not be sent: %+v", got)
	}
	a.ai.Plan.open = true
	a.ai.Plan.sql = "select * from users"
	got := a.aiPlanContext()
	if got == nil || !strings.Contains(got.Text, "Seq Scan") || got.SQL != "select * from users" {
		t.Fatalf("plan context: %+v", got)
	}
}

func TestAIDDLContextPrefetchAndCache(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "DDL")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.tree.ConnSchema[prof.ID] = "db1"
	fake := &schemaFakeSession{
		schemas: []string{"db1"},
		objects: map[string][]engine.DBObject{
			"db1": {{Kind: engine.ObjectTable, Name: "t", Schema: "db1"}},
		},
	}
	a.testSetSession(prof.ID, fake)
	a.ai.Prefs.AllowDDL = true
	a.ai.Prefs.Model = "fake"
	a.setActiveText("select * from t")
	a.session.Catalog.finish(prof.ID+"|db1", []string{"t"}, nil, "postgres")

	p := &fakeAIProvider{events: []ai.Event{{Kind: ai.EventText, Text: "ok"}, {Kind: ai.EventDone}}}
	a.ai.TestProvider = p
	a.ai.Chat.input = "看看这张表"
	a.sendAI()
	if fake.ddlSchema != "db1" {
		t.Fatalf("DDL must use the scoped schema: %q", fake.ddlSchema)
	}
	last := p.req.Messages[len(p.req.Messages)-1].Content
	if !strings.Contains(last, "CREATE TABLE db1.t") || !strings.Contains(last, "DDL:") {
		t.Fatalf("prompt missing DDL:\n%s", last)
	}

	// 第二次发送即使会话不可用，也应命中 DDL 缓存。
	a.session.Sessions = map[string]engine.Session{}
	a.ai.Chat.input = "再说一次"
	a.sendAI()
	last = p.req.Messages[len(p.req.Messages)-1].Content
	if !strings.Contains(last, "CREATE TABLE db1.t") {
		t.Fatalf("cached DDL missing on second send:\n%s", last)
	}
}

type fkFakeSession struct {
	treeFakeSession
	calls int
}

func (f *fkFakeSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapForeignKeys
}

func (f *fkFakeSession) ForeignKeys(_ context.Context, _ string, table string) ([]engine.ForeignKey, error) {
	f.calls++
	if table == "orders" {
		return []engine.ForeignKey{{Column: "customer_id", RefTable: "customers", RefColumn: "id"}}, nil
	}
	return nil, nil
}

func TestAIContextPrefetchesForeignKeys(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "FK")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select * from orders")
	a.session.Catalog.finish(prof.ID, []string{"orders", "customers", "metrics"}, nil, "")
	fk := &fkFakeSession{}
	a.testSetSession(prof.ID, fk)

	snap := a.chatSnapshot()
	var prepared ai.ContextSnapshot
	a.prepareAIContext("orders 有多少", snap, false, func(s ai.ContextSnapshot) { prepared = s })
	if fk.calls == 0 {
		t.Fatal("foreign keys should be loaded into the context")
	}
	var refs []ai.TableRef
	for _, table := range prepared.Tables {
		if table.Name == "orders" {
			refs = table.References
		}
	}
	if len(refs) != 1 || refs[0].Name != "customers" {
		t.Fatalf("orders references: %+v", refs)
	}

	// 第二次直接从缓存取，不再查询会话。
	before := fk.calls
	a.prepareAIContext("orders 有多少", a.chatSnapshot(), false, func(ai.ContextSnapshot) {})
	if fk.calls != before {
		t.Fatalf("foreign keys must be cached: calls %d -> %d", before, fk.calls)
	}
}
