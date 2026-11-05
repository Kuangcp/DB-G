package ui

import (
	"sort"
	"time"
)

// percentile returns the value at position p (0..1) of a sorted slice,
// using idx = int(p*(len-1)).
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Sampler is a bounded ring buffer of durations with percentile queries.
type Sampler struct {
	vals []time.Duration
	i, n int
}

func NewSampler(cap int) *Sampler {
	return &Sampler{vals: make([]time.Duration, cap)}
}

func (s *Sampler) Add(d time.Duration) {
	if len(s.vals) == 0 {
		return
	}
	s.vals[s.i] = d
	s.i = (s.i + 1) % len(s.vals)
	if s.n < len(s.vals) {
		s.n++
	}
}

// Sorted returns the samples in chronological-independent ascending order.
func (s *Sampler) Sorted() []time.Duration {
	if s.n == 0 {
		return nil
	}
	out := make([]time.Duration, 0, s.n)
	start := s.i - s.n
	if start < 0 {
		start += len(s.vals)
	}
	for k := 0; k < s.n; k++ {
		out = append(out, s.vals[(start+k)%len(s.vals)])
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out
}

func (s *Sampler) Median() time.Duration { return percentile(s.Sorted(), 0.5) }
func (s *Sampler) P90() time.Duration    { return percentile(s.Sorted(), 0.9) }
func (s *Sampler) P99() time.Duration    { return percentile(s.Sorted(), 0.99) }

// median returns the median of unsorted samples (测试与基准用）。
func median(v []time.Duration) time.Duration {
	s := append([]time.Duration(nil), v...)
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	return percentile(s, 0.5)
}

// pct returns percentile p of unsorted samples.
func pct(v []time.Duration, p float64) time.Duration {
	s := append([]time.Duration(nil), v...)
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	return percentile(s, p)
}
