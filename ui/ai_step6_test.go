package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"db-g/ai"
	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

type hydrateFakeSession struct {
	treeFakeSession
	tables      []engine.DBObject
	cols        map[string][]engine.Column
	objectCalls int
	columnCalls int
}

func (f *hydrateFakeSession) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns
}

func (f *hydrateFakeSession) Objects(_ context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	f.objectCalls++
	var out []engine.DBObject
	for _, o := range f.tables {
		if o.Kind == kind {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *hydrateFakeSession) Columns(_ context.Context, table string) ([]engine.Column, error) {
	f.columnCalls++
	return f.cols[table], nil
}

func TestAIContextHydratesTablesAndColumns(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "hydrate")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	fake := &hydrateFakeSession{
		tables: []engine.DBObject{
			{Kind: engine.ObjectTable, Name: "users"},
			{Kind: engine.ObjectTable, Name: "orders"},
		},
		cols: map[string][]engine.Column{
			"users":  {{Name: "id", Type: "INTEGER", PrimaryKey: true}, {Name: "name", Type: "TEXT"}},
			"orders": {{Name: "id", Type: "INTEGER"}, {Name: "user_id", Type: "INTEGER"}},
		},
	}
	a.testSetSession(prof.ID, fake)
	a.ai.Chat.input = "users 有哪些列"

	snap := a.chatSnapshot()
	if len(snap.Tables) != 0 {
		t.Fatalf("precondition: catalog/tree should be empty: %+v", snap.Tables)
	}
	var got ai.ContextSnapshot
	a.prepareAIContext("users 有哪些列", snap, false, func(s ai.ContextSnapshot) { got = s })
	if fake.objectCalls == 0 || fake.columnCalls == 0 {
		t.Fatalf("hydrate calls: objects=%d columns=%d", fake.objectCalls, fake.columnCalls)
	}
	if len(got.Tables) != 2 {
		t.Fatalf("hydrated tables: %+v", got.Tables)
	}
	found := false
	for _, table := range got.Tables {
		if table.Name == "users" && len(table.Columns) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("columns missing: %+v", got.Tables)
	}
	// 负载回填 catalog，下一次请求不再查库。
	if tables, _, ok := a.session.Catalog.objects(prof.ID); !ok || len(tables) != 2 {
		t.Fatalf("catalog not warmed: %v %v", tables, ok)
	}
	objects, columns := fake.objectCalls, fake.columnCalls
	a.prepareAIContext("users 有哪些列", a.chatSnapshot(), false, func(ai.ContextSnapshot) {})
	if fake.objectCalls != objects || fake.columnCalls != columns {
		t.Fatalf("second prepare must use cache: objects %d->%d columns %d->%d",
			objects, fake.objectCalls, columns, fake.columnCalls)
	}
}

func TestAISnapshotFallsBackToTreeData(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "tree")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.tree.Data[prof.ID] = map[engine.ObjectKind][]engine.DBObject{
		engine.ObjectTable: {{Kind: engine.ObjectTable, Name: "tree_t"}},
	}
	snap := a.aiSnapshot()
	if len(snap.Tables) != 1 || snap.Tables[0].Name != "tree_t" {
		t.Fatalf("tree fallback: %+v", snap.Tables)
	}
}

func TestWatchAIStreamIdleTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan ai.Event, 1)
	ch <- ai.Event{Kind: ai.EventText, Text: "partial"}
	out := watchAIStream(ctx, cancel, ch, 20*time.Millisecond)
	first := <-out
	if first.Kind != ai.EventText {
		t.Fatalf("first event: %+v", first)
	}
	second, ok := <-out
	if !ok || second.Kind != ai.EventError || !strings.Contains(second.Err.Error(), locales.AiTimeout()) {
		t.Fatalf("idle timeout event: %+v ok=%v", second, ok)
	}
}

type retryProvider struct{ calls int }

func (p *retryProvider) Models(context.Context) ([]string, error) { return nil, nil }
func (p *retryProvider) Stream(context.Context, ai.Request) (<-chan ai.Event, error) {
	p.calls++
	if p.calls == 1 {
		return nil, errors.New("connect refused")
	}
	ch := make(chan ai.Event, 1)
	ch <- ai.Event{Kind: ai.EventDone}
	close(ch)
	return ch, nil
}

func TestOpenAIStreamRetriesOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &retryProvider{}
	ch, err := openAIStream(ctx, cancel, p, ai.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		if ev.Kind != ai.EventDone {
			t.Fatalf("event: %+v", ev)
		}
	}
	if p.calls != 2 {
		t.Fatalf("retry calls = %d", p.calls)
	}
}

func TestAIHistoryRedaction(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "redact")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.history = app.OpenHistory(t.TempDir())
	if err := a.history.Add(app.HistoryEntry{
		ConnID: prof.ID, SQL: "connect password=letmein", Err: "Bearer abc.def",
	}); err != nil {
		t.Fatal(err)
	}
	a.ai.Prefs.AllowHistory = true
	a.ai.Prefs.AllowErrorLogs = true
	a.consoleErr = "token: xyz"
	snap := a.aiSnapshot()
	if len(snap.History) != 1 {
		t.Fatalf("history: %+v", snap.History)
	}
	joined := snap.History[0].SQL + snap.History[0].Err + snap.Errors[0].Err
	for _, leak := range []string{"letmein", "abc.def", "xyz"} {
		if strings.Contains(joined, leak) {
			t.Fatalf("leaked %q: %s", leak, joined)
		}
	}
	if !strings.Contains(joined, "***") {
		t.Fatalf("redaction marker missing: %s", joined)
	}
}
