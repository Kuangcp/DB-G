package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

type blockingFake struct{}

func (blockingFake) Open(context.Context) error { return nil }
func (blockingFake) Close() error               { return nil }
func (blockingFake) Caps() engine.Capabilities  { return 0 }
func (blockingFake) Cancel() bool               { return false }
func (blockingFake) RunStatement(ctx context.Context, _ string) (*engine.QueryResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func newTestRoot() *root {
	return newRoot("", strats{scopeViewport, scopeViewport})
}

func TestResultPaneEmpty(t *testing.T) {
	a := newTestRoot()
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("请先连接数据源") {
		t.Fatalf("empty hint missing: %q", tt.Texts())
	}
}

func TestResultPaneTable(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true,
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "alice"}, {int64(2), nil}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	for range 3 {
		tt.Frame()
	}
	for _, want := range []string{"id", "name", "alice", "NULL"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestResultPaneTruncatedAndAffected(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(&engine.QueryResult{
		IsQuery: true, Truncated: true,
		Columns: []string{"v"},
		Rows:    [][]any{{"x"}},
	}, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("结果已截断") {
		t.Fatalf("truncation badge missing: %q", tt.Texts())
	}

	a.con.SetOutcome(&engine.QueryResult{Affected: 7}, "")
	tt.Frame()
	if !tt.HasText("7 行受影响") {
		t.Fatalf("affected text missing: %q", tt.Texts())
	}
}

func TestResultPaneErrorAndStop(t *testing.T) {
	a := newTestRoot()
	a.con.SetOutcome(nil, "no such table: missing")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if !tt.HasText("no such table: missing") {
		t.Fatalf("error banner missing: %q", tt.Texts())
	}

	a.con.SetOutcome(nil, "")
	a.setSession("test", blockingFake{})
	if !a.con.Run(a.sess, "SELECT 1") {
		t.Fatal("run must start")
	}
	tt.Frame()
	if !tt.HasText("执行中") {
		t.Fatalf("running state missing: %q", tt.Texts())
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
}

func TestCellTextAndCutRunes(t *testing.T) {
	if got := cellText(nil); got != "NULL" {
		t.Fatalf("nil → %q", got)
	}
	if got := cellText([]byte{1, 2, 3}); got != "[3 bytes]" {
		t.Fatalf("binary []byte → %q", got)
	}
	// MySQL 的 TEXT/VARCHAR 以 []byte 返回：必须渲染真实文本，而不是字节数摘要。
	if got := cellText([]byte("hello 世界")); got != "hello 世界" {
		t.Fatalf("text []byte → %q", got)
	}
	if got := cellText([]byte("a\x00b")); got != "[3 bytes]" {
		t.Fatalf("NUL bytes must stay binary → %q", got)
	}
	if got := cellText([]byte{0xff, 0xfe}); got != "[2 bytes]" {
		t.Fatalf("invalid UTF-8 must stay binary → %q", got)
	}
	longBytes := []byte(strings.Repeat("好", 600))
	cut := cellText(longBytes)
	if !strings.HasSuffix(cut, "…") || len([]rune(cut)) != 513 {
		t.Fatalf("long text []byte must be cut like string, runes=%d", len([]rune(cut)))
	}
	if got := cellText(int64(42)); got != "42" {
		t.Fatalf("int64 → %q", got)
	}
	if got := cutRunes("short", 512); got != "short" {
		t.Fatalf("short cut = %q", got)
	}
	long := strings.Repeat("好", 600)
	got := cutRunes(long, 512)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 513 {
		t.Fatalf("cutRunes runes = %d, suffix ok=%v", len([]rune(got)), strings.HasSuffix(got, "…"))
	}
}
