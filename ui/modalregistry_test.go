package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestModalRegistryDrawsInOrder(t *testing.T) {
	var reg modalRegistry
	var got []int
	reg.add(func(*ui.Context) { got = append(got, 1) })
	reg.add(func(*ui.Context) { got = append(got, 2) })
	reg.add(nil)
	reg.draw(nil)
	if want := []int{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// 注册进 registry 的弹窗每帧都会被询问一次，不需要再改 view 的手写列表。
func TestViewDrawsRegisteredModals(t *testing.T) {
	a := newConsoleRoot(t)
	calls := 0
	a.modals.add(func(*ui.Context) { calls++ })
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	first := calls
	if first == 0 {
		t.Fatal("registered modal was not drawn")
	}
	tt.Frame()
	if calls <= first {
		t.Fatalf("registered modal must keep drawing: before=%d after=%d", first, calls)
	}
}
