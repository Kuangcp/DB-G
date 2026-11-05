package app

import (
	"context"
	"sync"
	"time"

	"db-g/engine"
)

const queryTimeout = 30 * time.Second

// Console 承载一条控制台的执行状态；所有字段由 mu 保护。
type Console struct {
	mu      sync.Mutex
	publish func(func())
	gen     uint64
	running bool
	result  *engine.QueryResult
	err     string
	cancel  context.CancelFunc
	onDone  func(res *engine.QueryResult, errMsg string, dur time.Duration)
	dur     time.Duration
}

func NewConsole() *Console {
	return &Console{publish: func(func()) {}}
}

// SetOnDone 注册「一次执行结束」的回调（异步，主线程之外调用；用于日志/诊断）。
func (c *Console) SetOnDone(f func(res *engine.QueryResult, errMsg string, dur time.Duration)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDone = f
}

func (c *Console) SetPublisher(f func(func())) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publish = f
}

func (c *Console) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *Console) Result() *engine.QueryResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

// Dur 返回最近一次执行的耗时。
func (c *Console) Dur() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dur
}

func (c *Console) Err() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// SetOutcome 注入结果（测试与性能基准用）。
func (c *Console) SetOutcome(res *engine.QueryResult, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running, c.result, c.err = false, res, errMsg
}

// Run 在后台协程用 sess 执行 query；无会话、已在执行或空串时返回 false。
func (c *Console) Run(sess engine.Session, query string) bool {
	c.mu.Lock()
	if sess == nil || c.running || query == "" {
		c.mu.Unlock()
		return false
	}
	c.gen++
	gen := c.gen
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	c.cancel = cancel
	c.running, c.result, c.err = true, nil, ""
	c.mu.Unlock()

	start := time.Now()
	go func() {
		res, err := sess.RunStatement(ctx, query)
		cancel()
		dur := time.Since(start)
		var done func(*engine.QueryResult, string, time.Duration)
		c.mu.Lock()
		if gen == c.gen {
			c.dur = dur
			c.running, c.cancel = false, nil
			if err != nil {
				c.result, c.err = nil, err.Error()
			} else {
				c.result, c.err = res, ""
			}
			done = c.onDone
		}
		pubFn := c.publish
		c.mu.Unlock()
		pubFn(func() {})
		if done != nil {
			done(c.Result(), c.Err(), dur)
		}
	}()
	return true
}

// Cancel 取消当前执行；完成回调把取消错误写入 Err。
func (c *Console) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
