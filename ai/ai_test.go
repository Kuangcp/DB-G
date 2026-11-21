package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderStream(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"id":"1","choices":[{"index":0,"delta":{"content":"SELECT "},"finish_reason":null}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{"content":"1;"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL + "/v1", APIKey: "test"})
	ch, err := p.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var usage *Usage
	for ev := range ch {
		switch ev.Kind {
		case EventText:
			text.WriteString(ev.Text)
		case EventUsage:
			usage = ev.Usage
		case EventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path: %q", gotPath)
	}
	if text.String() != "SELECT 1;" {
		t.Fatalf("text: %q", text.String())
	}
	if usage == nil || usage.TotalTokens != 7 {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestOpenAIProviderModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "b-model", "object": "model"},
				{"id": "a-model", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL + "/v1"})
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(models, ",") != "a-model,b-model" {
		t.Fatalf("models: %v", models)
	}
}

func TestBuildMessagesRanksTablesAndKeepsEgress(t *testing.T) {
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql", Database: "shop"},
		ActiveSQL:  "select * from users",
		Tables: []TableContext{
			{Name: "users", Columns: []ColumnContext{{Name: "id", Type: "bigint"}}},
			{Name: "orders", Columns: []ColumnContext{{Name: "amount", Type: "int"}}},
			{Name: "audit_log"},
		},
		History: []HistoryItem{{SQL: "select 1"}},
	}
	msgs := BuildMessages(snapshot, "统计 users 的订单")
	if len(msgs) != 2 || msgs[0].Role != RoleSystem || msgs[1].Role != RoleUser {
		t.Fatalf("messages: %+v", msgs)
	}
	body := msgs[1].Content
	for _, want := range []string{"MySQL", "users", "orders", "select * from users"} {
		if !strings.Contains(body, want) {
			t.Fatalf("context missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Recent Queries") {
		t.Fatalf("history should be included when the snapshot carries it:\n%s", body)
	}
}

func TestRankTables(t *testing.T) {
	tables := []TableContext{
		{Name: "audit_log"},
		{Name: "user_profile", Columns: []ColumnContext{{Name: "nickname"}}},
		{Name: "orders"},
	}
	got := RankTables("查 user_profile 的 nickname", tables, "", 2)
	if len(got) != 2 || got[0].Name != "user_profile" {
		t.Fatalf("ranked: %+v", got)
	}
}

func TestExtractSQL(t *testing.T) {
	text := "说明\n```sql\nSELECT 1;\n```\n还有\n```\nSELECT 2;\n```"
	got, ok := ExtractSQL(text)
	if !ok || got != "SELECT 1;" {
		t.Fatalf("extract: %q %v", got, ok)
	}
	if _, ok := ExtractSQL("no code"); ok {
		t.Fatal("no code block must return false")
	}
}

func TestBuildFixMessages(t *testing.T) {
	snapshot := ContextSnapshot{Connection: ConnInfo{Kind: "postgres"}}
	msgs := BuildFixMessages(snapshot, "select * from user", "column \"user\" does not exist")
	if len(msgs) != 2 {
		t.Fatalf("messages: %+v", msgs)
	}
	body := msgs[1].Content
	for _, want := range []string{"select * from user", "does not exist", "PostgreSQL"} {
		if !strings.Contains(body, want) {
			t.Fatalf("fix context missing %q:\n%s", want, body)
		}
	}
}

func TestBuildCompletionMessages(t *testing.T) {
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql", Database: "shop"},
		Tables:     []TableContext{{Name: "users", Columns: []ColumnContext{{Name: "id", Type: "bigint"}}}},
	}
	msgs := BuildCompletionMessages(snapshot, "select * from us", " where id = 1")
	if len(msgs) != 2 {
		t.Fatalf("messages: %+v", msgs)
	}
	body := msgs[1].Content
	for _, want := range []string{"select * from us", "<CURSOR>", "where id = 1", "users"} {
		if !strings.Contains(body, want) {
			t.Fatalf("completion prompt missing %q:\n%s", want, body)
		}
	}
	if got := ExtractCompletion("```sql\nFROM users\n```"); got != "FROM users" {
		t.Fatalf("extract completion: %q", got)
	}
}

func TestBuildRewriteMessagesKeepsTargetOnce(t *testing.T) {
	target := "select * from users where id = 1"
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql", Database: "shop"},
		ActiveSQL:  target,
		Selection:  target,
		Tables:     []TableContext{{Name: "users", Columns: []ColumnContext{{Name: "id"}}}},
	}
	msgs := BuildRewriteMessages(snapshot, target, "只保留需要的列")
	if len(msgs) != 2 {
		t.Fatalf("messages: %+v", msgs)
	}
	body := msgs[1].Content
	for _, want := range []string{"只保留需要的列", "# SQL to Rewrite", target, "users"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rewrite prompt missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, target) != 1 {
		t.Fatalf("target should appear once, got %d:\n%s", strings.Count(body, target), body)
	}
	if strings.Contains(body, "# Current Selection") || strings.Contains(body, "# Current SQL") {
		t.Fatalf("duplicate current-SQL sections:\n%s", body)
	}
}

func TestRankTablesUnicodeAndForeignKeyExpansion(t *testing.T) {
	tables := []TableContext{
		{Name: "订单", Columns: []ColumnContext{{Name: "用户ID"}, {Name: "金额"}}},
		{Name: "用户", Columns: []ColumnContext{{Name: "用户ID"}, {Name: "昵称"}}},
		{Name: "日志"},
		{Name: "audit_log"},
	}
	got := RankTables("查用户订单的金额", tables, "", 3)
	if got[0].Name != "订单" {
		t.Fatalf("Chinese query ranking: %+v", got)
	}
	if got[0].Name == "订单" && len(got) > 1 && got[1].Name != "用户" {
		t.Fatalf("related Chinese table should follow: %+v", got)
	}

	// orders 命中后，外键指向的 customers 应被带到无关表前面。
	refs := []TableContext{
		{Name: "orders", Columns: []ColumnContext{{Name: "customer_id"}},
			References: []TableRef{{Name: "customers"}}},
		{Name: "metrics"},
		{Name: "customers"},
	}
	ranked := RankTables("orders 有多少", refs, "", 2)
	if len(ranked) != 2 || ranked[0].Name != "orders" || ranked[1].Name != "customers" {
		t.Fatalf("foreign-key expansion: %+v", ranked)
	}
}

func TestBuildChatIncludesDialogueSummary(t *testing.T) {
	msgs, _ := BuildChat(ContextSnapshot{
		Connection:      ConnInfo{Kind: "sqlite"},
		DialogueSummary: "更早的对话：用户问过 users 表结构",
	}, "现在呢", Budget{})
	if !strings.Contains(msgs[0].Content, "Earlier conversation summary") || !strings.Contains(msgs[0].Content, "users 表结构") {
		t.Fatalf("system prompt missing summary:\n%s", msgs[0].Content)
	}
}

func TestQueryTokensUnicode(t *testing.T) {
	tokens := queryTokens("查询用户订单 user_profile")
	joined := strings.Join(tokens, ",")
	for _, want := range []string{"用户", "订单", "user", "profile"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, tokens)
		}
	}
}

func TestPromptFenceEscapesBackticks(t *testing.T) {
	target := "select '```sql'"
	msgs := BuildRewriteMessages(ContextSnapshot{
		Connection: ConnInfo{Kind: "sqlite"},
		Tables:     []TableContext{{Name: "t", DDL: "CREATE TABLE t (v TEXT DEFAULT '```')"}},
	}, target, "保持语义")
	body := msgs[1].Content
	if !strings.Contains(body, "````sql") {
		t.Fatalf("DDL fence must grow for embedded backticks:\n%s", body)
	}
	if !strings.Contains(body, "````sql\n"+target+"\n````") {
		t.Fatalf("rewrite target fence must grow:\n%s", body)
	}
}
