package ui

import (
	"slices"
	"testing"
)

// 帧首统一执行延迟动作：FIFO、执行期间新增的动作也会被排空、同一动作不会重复执行。
func TestDeferredActionsRunInOrderOnce(t *testing.T) {
	a := newRoot("")
	var got []int
	a.deferUI(func() { got = append(got, 1) })
	a.deferUI(func() { got = append(got, 2) })
	a.deferUI(func() {
		got = append(got, 3)
		a.deferUI(func() { got = append(got, 4) })
	})

	a.drainDeferred()
	if want := []int{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if len(a.deferred) != 0 {
		t.Fatalf("queue not drained: %d", len(a.deferred))
	}

	a.drainDeferred()
	if want := []int{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("actions ran twice: %v", got)
	}
}
