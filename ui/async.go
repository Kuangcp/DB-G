package ui

import (
	"fmt"

	"db-g/app"
)

// nextAsyncSeq 为同一 key 的异步请求分配递增轮次；旧轮次的结果由调用方丢弃。
func (a *root) nextAsyncSeq(key string) uint64 {
	if a.session.AsyncSeq == nil {
		a.session.AsyncSeq = map[string]uint64{}
	}
	a.session.AsyncSeq[key]++
	return a.session.AsyncSeq[key]
}

// runAsync 是统一的异步执行入口：
//   - headless（无窗口、无注入）同步执行，测试可立即断言；
//   - 有窗口时后台执行，safeUpdate 回投 UI 线程；
//   - key 非空时按轮次丢弃过期结果；
//   - work panic 转成 error 交给 apply，不让进程退出。
func runAsync[T any](a *root, key string, work func() (T, error), apply func(T, error)) {
	var seq uint64
	if key != "" {
		seq = a.nextAsyncSeq(key)
	}
	var (
		val T
		err error
	)
	a.runBackground(func() {
		val, err = safeAsyncWork(work)
	}, func() {
		if key != "" && a.session.AsyncSeq[key] != seq {
			return
		}
		apply(val, err)
	})
}

func safeAsyncWork[T any](work func() (T, error)) (val T, err error) {
	defer func() {
		if r := recover(); r != nil {
			app.LogError("panic", "async work", "err", fmt.Sprint(r))
			err = fmt.Errorf("async work panic: %v", r)
		}
	}()
	return work()
}
