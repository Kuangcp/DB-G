package app

import (
	"path/filepath"
	"testing"
)

func TestConsolePrefsDefaultAndNormalize(t *testing.T) {
	if got := DefaultConsolePrefs().SaveDelaySeconds; got != 5 {
		t.Fatalf("default delay = %d, want 5", got)
	}
	cases := map[int]int{
		0:   DefaultConsoleSaveDelaySeconds,
		-1:  MinConsoleSaveDelaySeconds,
		1:   1,
		12:  12,
		999: MaxConsoleSaveDelaySeconds,
	}
	for in, want := range cases {
		if got := NormalizeConsolePrefs(ConsolePrefs{SaveDelaySeconds: in}).SaveDelaySeconds; got != want {
			t.Fatalf("Normalize(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestConsolePrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.json")
	prefs, warn := LoadConsolePrefs(path)
	if warn != "" || prefs.SaveDelaySeconds != 5 {
		t.Fatalf("missing file: %+v warn=%q", prefs, warn)
	}
	if err := SaveConsolePrefs(path, ConsolePrefs{SaveDelaySeconds: 12}); err != nil {
		t.Fatal(err)
	}
	prefs, warn = LoadConsolePrefs(path)
	if warn != "" || prefs.SaveDelaySeconds != 12 {
		t.Fatalf("load: %+v warn=%q", prefs, warn)
	}
}
