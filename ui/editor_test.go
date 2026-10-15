package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEditorInjection(t *testing.T) {
	text := buildSQL(3_000)
	for _, sc := range []strats{
		{scopeFull, scopeFull},         // FF
		{scopeFull, scopeViewport},     // FV
		{scopeViewport, scopeViewport}, // VV
	} {
		a := newRoot(text, sc)
		tt := ui.NewTester(rootView(a), 900, 640)
		for range 3 {
			tt.Frame()
		}
		if a.ed.Injected <= 0 {
			t.Fatalf("%+v: injected=%d want >0", sc, a.ed.Injected)
		}
		if sc.inject == scopeFull && sc.tokenize == scopeFull && a.ed.Injected < 10_000 {
			t.Fatalf("FF expected many ranges, got %d", a.ed.Injected)
		}
		if sc.inject == scopeViewport && a.ed.Injected > 20_000 {
			t.Fatalf("viewport injection blew up: %d", a.ed.Injected)
		}
		if len(a.ed.allRanges) == 0 {
			t.Fatalf("%+v: empty allRanges", sc)
		}
	}
}

func TestEditorTypingKeepsColors(t *testing.T) {
	a := newRoot(buildSQL(200), strats{scopeViewport, scopeViewport})
	tt := ui.NewTester(rootView(a), 900, 640)
	for range 2 {
		tt.Frame()
	}
	if err := tt.Click("编辑器"); err != nil {
		t.Fatalf("focus editor: %v", err)
	}
	tt.Type("x")
	tt.Frame()
	if a.ed.Injected == 0 {
		t.Fatal("no ranges after typing")
	}
	if !strings.Contains(a.ed.Buf.String(), "x") {
		t.Fatal("typed char missing")
	}
}