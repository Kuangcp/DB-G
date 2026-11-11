package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/egoist/mygo"
)

func TestWindowStateStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db-g", "window-state.json")
	s := windowStateStore{path: path}
	if states, err := s.LoadWindowStates(); err != nil || states != nil {
		t.Fatalf("missing file: %v %v", states, err)
	}
	want := map[string]mygo.WindowState{
		"main": {X: 120, Y: 80, Width: 1440, Height: 900, Maximized: true},
	}
	if err := s.SaveWindowStates(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadWindowStates()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %+v, want %+v", got, want)
	}
	if err := s.SaveWindowStates(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty states must remove the file: %v", err)
	}
}

func TestWindowStateStoreCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (windowStateStore{path: path}).LoadWindowStates(); err == nil {
		t.Fatal("corrupt state must report an error")
	}
}
