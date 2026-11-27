package ui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"db-g/engine"
)

type cancelCountSession struct {
	started     chan struct{}
	cancelCalls atomic.Int32
}

func (s *cancelCountSession) Open(context.Context) error { return nil }
func (s *cancelCountSession) Close() error               { return nil }
func (s *cancelCountSession) Caps() engine.Capabilities  { return 0 }

func (s *cancelCountSession) RunStatement(ctx context.Context, _ string) (*engine.QueryResult, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// 会话级 Cancel 不应再被调用：停止只能通过每条语句自己的 context。
func (s *cancelCountSession) Cancel() bool {
	s.cancelCalls.Add(1)
	return false
}

func TestStopRunsCancelsContextOnly(t *testing.T) {
	a := newConsoleRoot(t)
	sess := &cancelCountSession{started: make(chan struct{})}
	a.session.Sessions["c1"] = sess
	a.touchSession("c1")
	if !a.con.Run(sess, "SELECT slow") {
		t.Fatal("run must start")
	}
	select {
	case <-sess.started:
	case <-time.After(2 * time.Second):
		t.Fatal("statement did not start")
	}
	a.stopActiveRuns()
	waitFor(t, 2*time.Second, func() bool { return !a.con.Running() })
	if got := sess.cancelCalls.Load(); got != 0 {
		t.Fatalf("Session.Cancel called %d times, want 0", got)
	}
	if a.con.Err() == "" {
		t.Fatal("canceled run must surface an error")
	}
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
	t.Fatal("condition not met in time")
}
