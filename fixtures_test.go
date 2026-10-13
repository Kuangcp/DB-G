package main

import (
	"strings"
	"testing"
)

func TestFixtures(t *testing.T) {
	for _, n := range []int{3_000, 12_000, 100_000} {
		s := buildSQL(n)
		if got := strings.Count(s, "\n"); got != n {
			t.Fatalf("lines=%d want %d", got, n)
		}
		for _, tok := range []string{"INSERT", "SELECT", "'华东大区'", "-- "} {
			if !strings.Contains(s, tok) {
				t.Fatalf("fixture missing %q", tok)
			}
		}
	}
}