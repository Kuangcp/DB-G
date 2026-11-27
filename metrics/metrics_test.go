package metrics

import (
	"testing"
	"time"
)

func TestSamplerAndPercentiles(t *testing.T) {
	s := NewSampler(3)
	for _, d := range []time.Duration{3, 1, 2, 4, 5} {
		s.Add(d * time.Millisecond)
	}
	if got := s.Median(); got != 4*time.Millisecond {
		t.Fatalf("median = %v", got)
	}
	if got := s.P99(); got != 4*time.Millisecond {
		t.Fatalf("p99 = %v", got)
	}
	xs := []time.Duration{1, 2, 3, 4}
	if Percentile([]time.Duration{1, 2, 3}, 0.5) != 2 {
		t.Fatal("percentile")
	}
	if Median(xs) != 2 {
		t.Fatalf("median = %v", Median(xs))
	}
	if Pct(xs, 0.99) != 3 {
		t.Fatalf("pct = %v", Pct(xs, 0.99))
	}
}
