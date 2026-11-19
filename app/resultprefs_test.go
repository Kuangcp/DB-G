package app

import (
	"path/filepath"
	"testing"
)

func TestResultPrefsDefaultAndNormalize(t *testing.T) {
	if got := DefaultResultPrefs().BudgetMB; got != 8 {
		t.Fatalf("default budget = %d, want 8", got)
	}
	if got := DefaultResultPrefs().PageSize; got != DefaultResultPageSize {
		t.Fatalf("default page size = %d, want %d", got, DefaultResultPageSize)
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
	pageCases := map[int]int{
		0:    DefaultResultPageSize,
		-1:   MinResultPageSize,
		10:   MinResultPageSize,
		500:  500,
		9999: MaxResultPageSize,
	}
	for in, want := range pageCases {
		if got := NormalizeResultPrefs(ResultPrefs{PageSize: in}).PageSize; got != want {
			t.Fatalf("Normalize page %d = %d, want %d", in, got, want)
		}
	}
}

func TestResultPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	if prefs, warn := LoadResultPrefs(path); warn != "" || prefs.BudgetMB != 8 || prefs.PageSize != DefaultResultPageSize {
		t.Fatalf("missing file: %+v warn=%q", prefs, warn)
	}
	if err := SaveResultPrefs(path, ResultPrefs{BudgetMB: 12, PageSize: 800}); err != nil {
		t.Fatal(err)
	}
	prefs, warn := LoadResultPrefs(path)
	if warn != "" || prefs.BudgetMB != 12 || prefs.PageSize != 800 {
		t.Fatalf("load: %+v warn=%q", prefs, warn)
	}
}
