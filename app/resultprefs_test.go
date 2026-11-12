package app

import (
	"path/filepath"
	"testing"
)

func TestResultPrefsDefaultAndNormalize(t *testing.T) {
	if got := DefaultResultPrefs().BudgetMB; got != 8 {
		t.Fatalf("default budget = %d, want 8", got)
	}
	cases := map[int]int{
		0:    DefaultResultBudgetMB,
		-1:   MinResultBudgetMB,
		1:    1,
		32:   32,
		9999: MaxResultBudgetMB,
	}
	for in, want := range cases {
		if got := NormalizeResultPrefs(ResultPrefs{BudgetMB: in}).BudgetMB; got != want {
			t.Fatalf("Normalize(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestResultPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	if prefs, warn := LoadResultPrefs(path); warn != "" || prefs.BudgetMB != 8 {
		t.Fatalf("missing file: %+v warn=%q", prefs, warn)
	}
	if err := SaveResultPrefs(path, ResultPrefs{BudgetMB: 12}); err != nil {
		t.Fatal(err)
	}
	prefs, warn := LoadResultPrefs(path)
	if warn != "" || prefs.BudgetMB != 12 {
		t.Fatalf("load: %+v warn=%q", prefs, warn)
	}
}
