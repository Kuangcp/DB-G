package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"db-g/engine"
)

type fakeSession struct {
	mu    sync.Mutex
	got   string
	block chan struct{}
}

func (f *fakeSession) Open(context.Context) error { return nil }
func (f *fakeSession) Close() error                { return nil }
func (f *fakeSession) Caps() engine.Capabilities   { return 0 }
func (f *fakeSession) Cancel() bool                { return false }

func (f *fakeSession) RunStatement(ctx context.Context, q string) (*engine.QueryResult, error) {
	f.mu.Lock()
	f.got = q
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
			return &engine.QueryResult{IsQuery: true, Columns: []string{"ok"}, Rows: [][]any{{"y"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &engine.QueryResult{Affected: 3}, nil
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

func TestRunCompletes(t *testing.T) {
	f := &fakeSession{}
	c := NewConsole()
	if !c.Run(f, "UPDATE t SET x=1") {
		t.Fatal("Run must start")
	}
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Err() != "" {
		t.Fatalf("err: %s", c.Err())
	}
	if got := c.Result(); got == nil || got.Affected != 3 {
		t.Fatalf("result: %+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.got != "UPDATE t SET x=1" {
		t.Fatalf("sql = %q", f.got)
	}
}

func TestRunRequiresSession(t *testing.T) {
	c := NewConsole()
	if c.Run(nil, "SELECT 1") {
		t.Fatal("nil session must refuse")
	}
}

func TestRunGuards(t *testing.T) {
	f := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	if !c.Run(f, "SELECT 1") {
		t.Fatal("first run must start")
	}
	if c.Run(f, "SELECT 2") {
		t.Fatal("second run while running must refuse")
	}
	close(f.block)
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Run(f, "") {
		t.Fatal("empty query must refuse")
	}
}

func TestCancelStopsRun(t *testing.T) {
	f := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	if !c.Run(f, "SELECT slow") {
		t.Fatal("run must start")
	}
	c.Cancel()
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if !strings.Contains(c.Err(), "context canceled") {
		t.Fatalf("err = %q", c.Err())
	}
}

func TestCancelLeavesNoStaleResult(t *testing.T) {
	f := &fakeSession{block: make(chan struct{})}
	c := NewConsole()
	if !c.Run(f, "SELECT 1") {
		t.Fatal("first run")
	}
	if c.Run(f, "SELECT 2") {
		t.Fatal("a running console must reject a second run")
	}
	c.Cancel()
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Result() != nil || c.Err() == "" {
		t.Fatalf("cancel must surface an error and no result: %+v %q", c.Result(), c.Err())
	}
	f2 := &fakeSession{}
	if !c.Run(f2, "SELECT 3") {
		t.Fatal("must be runnable again after cancel")
	}
	waitFor(t, 2*time.Second, func() bool { return !c.Running() })
	if c.Err() != "" {
		t.Fatalf("err after rerun: %q", c.Err())
	}
}
