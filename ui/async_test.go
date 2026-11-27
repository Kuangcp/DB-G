package ui

import (
	"slices"
	"testing"
)

// headless 下 work/apply 同步执行，测试可在调用后立即断言。
func TestRunAsyncHeadlessIsSynchronous(t *testing.T) {
	a := newRoot("")
	var order []string
	runAsync(a, "k", func() (int, error) {
		order = append(order, "work")
		return 7, nil
	}, func(v int, err error) {
		order = append(order, "apply")
		if err != nil || v != 7 {
			t.Fatalf("v=%d err=%v", v, err)
		}
	})
	if want := []string{"work", "apply"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v", order)
	}
}

// 同一 key 的旧结果必须丢弃；新结果正常落地。
func TestRunAsyncDropsStaleResult(t *testing.T) {
	a := newRoot("")
	var queued []func()
	a.session.BgRun = func(work, done func()) {
		queued = append(queued, func() { work(); done() })
	}
	var got []int
	runAsync(a, "meta", func() (int, error) { return 1, nil }, func(v int, _ error) { got = append(got, v) })
	runAsync(a, "meta", func() (int, error) { return 2, nil }, func(v int, _ error) { got = append(got, v) })
	queued[0]() // 第一轮在第二轮之后完成
	if len(got) != 0 {
		t.Fatalf("stale applied: %v", got)
	}
	queued[1]()
	if want := []int{2}; !slices.Equal(got, want) {
		t.Fatalf("got = %v", got)
	}
}

// work panic 不能把进程带走；apply 收到错误。
func TestRunAsyncRecoversWorkPanic(t *testing.T) {
	a := newRoot("")
	var gotErr error
	runAsync(a, "", func() (int, error) { panic("boom") }, func(_ int, err error) { gotErr = err })
	if gotErr == nil {
		t.Fatal("panic must surface as error")
	}
}

// runBackground 的注入点仍然生效（连接异步测试依赖它）。
func TestRunBackgroundInjection(t *testing.T) {
	a := newRoot("")
	called := false
	a.session.BgRun = func(work, done func()) { called = true; work(); done() }
	a.runBackground(func() {}, func() {})
	if !called {
		t.Fatal("bgRun injection not used")
	}
}
