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
	sess    engine.Session
	gen     uint64
	running bool
	result  *engine.QueryResult
	err     string
	cancel  context.CancelFunc
}

func NewConsole() *Console {
	return &Console{publish: func(func()) {}}
}

func (c *Console) SetPublisher(f func(func())) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publish = f
}

func (c *Console) UseSession(s engine.Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sess = s
}

// SwapSession 取消在途执行、丢弃其结果并替换会话，同时关闭旧会话。
func (c *Console) SwapSession(s engine.Session) {
	c.mu.Lock()
	old := c.sess
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.gen++
	c.sess = s
	c.running, c.result, c.err = false, nil, ""
	pub := c.publish
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	pub(func() {})
}

func (c *Console) Session() engine.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess
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

// Run 在后台协程执行 query；无会话、已在执行或空串时返回 false。
func (c *Console) Run(query string) bool {
	c.mu.Lock()
	if c.sess == nil || c.running || query == "" {
		c.mu.Unlock()
		return false
	}
	c.gen++
	gen := c.gen
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	c.cancel = cancel
	c.running, c.result, c.err = true, nil, ""
	sess := c.sess
	c.mu.Unlock()

	go func() {
		res, err := sess.RunStatement(ctx, query)
		cancel()
		c.mu.Lock()
		if gen == c.gen {
			c.running, c.cancel = false, nil
			if err != nil {
				c.result, c.err = nil, err.Error()
			} else {
				c.result, c.err = res, ""
			}
		}
		pubFn := c.publish
		c.mu.Unlock()
		pubFn(func() {})
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
