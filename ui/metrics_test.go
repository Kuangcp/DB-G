package ui

import (
	"sort"
	"testing"
	"time"
)

func TestSampler(t *testing.T) {
	s := NewSampler(1000)
	for i := 0; i < 100; i++ {
		s.Add(time.Duration(i+1) * time.Millisecond)
	}
	if s.Median() != 50*time.Millisecond {
		t.Fatalf("median=%v want 50ms", s.Median())
	}
	if s.P90() < 89*time.Millisecond || s.P90() > 91*time.Millisecond {
		t.Fatalf("p90=%v", s.P90())
	}
	if s.P99() != 99*time.Millisecond {
		t.Fatalf("p99=%v", s.P99())
	}
}

func TestSamplerBounded(t *testing.T) {
	s := NewSampler(10)
	for i := 0; i < 25; i++ {
		s.Add(time.Duration(i) * time.Millisecond)
	}
	got := s.Sorted()
	if len(got) != 10 {
		t.Fatalf("cap not honored: %d", len(got))
	}
	// ring keeps the newest 10: 15ms..24ms
	if got[0] != 15*time.Millisecond || got[9] != 24*time.Millisecond {
		t.Fatalf("ring order wrong: first=%v last=%v", got[0], got[9])
	}
}

func TestPercentileHelper(t *testing.T) {
	xs := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	if percentile(xs, 0.5) != 3*time.Millisecond {
		t.Fatalf("p50=%v", percentile(xs, 0.5))
	}
	if percentile(nil, 0.5) != 0 {
		t.Fatal("empty must be 0")
	}
}
