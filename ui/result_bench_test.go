package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func TestResultGridFrameBudget(t *testing.T) {
	const (
		rows = 100_000
		cols = 50
	)
	labels := make([]any, cols)
	for j := range labels {
		labels[j] = fmt.Sprintf("col_%02d_value", j)
	}
	r := &engine.QueryResult{
		IsQuery: true,
		Columns: make([]string, cols),
		Rows:    make([][]any, rows),
	}
	for j := range r.Columns {
		r.Columns[j] = fmt.Sprintf("col_%02d", j)
	}
	for i := range r.Rows {
		row := make([]any, cols)
		copy(row, labels)
		r.Rows[i] = row
	}

	a := newRoot("", strats{scopeViewport, scopeViewport})
	a.con.SetOutcome(r, "")
	tt := ui.NewTester(rootView(a), 1600, 900)
	for range 5 {
		tt.Frame()
	}
	var samples []time.Duration
	for range 30 {
		t0 := time.Now()
		tt.Frame()
		samples = append(samples, time.Since(t0))
	}
	p50, p99 := median(samples), pct(samples, 0.99)
	t.Logf("result grid 100k x %d: frame p50=%v p99=%v", cols, p50, p99)
	if p50 > 16*time.Millisecond {
		t.Errorf("frame p50 %v exceeds 16ms budget", p50)
	}
}
