package app

import (
	"context"
	"sync"
	"time"

	"db-g/engine"
	"db-g/locales"
)

const queryTimeout = 30 * time.Second

// Console 承载一条控制台的执行状态；所有字段由 mu 保护。
type Console struct {
	mu        sync.Mutex
	publish   func(func())
	gen       uint64
	running   bool
	canceling bool
	result    *engine.QueryResult
	err       string
	cancel    context.CancelFunc
	onDone    func(res *engine.QueryResult, errMsg string, dur time.Duration)
	dur       time.Duration
	progRows  int
	progBytes int64
	progAt    time.Time
	pageSize  int
	budgetMB  int
}

func NewConsole() *Console {
	return &Console{publish: func(func()) {}, pageSize: engine.DefaultPageSize, budgetMB: engine.DefaultResultBudgetMB}
}

// SetPageSize 设置本运行态执行语句时单页读取的行数。
func (c *Console) SetPageSize(n int) {
	if n <= 0 {
		n = engine.DefaultPageSize
	}
	c.mu.Lock()
	c.pageSize = n
	c.mu.Unlock()
}

// PageSize 返回本运行态的每页行数。
func (c *Console) PageSize() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pageSize
}

// SetResultBudgetMB 设置本运行态执行语句时的结果大小预算（MB）。
func (c *Console) SetResultBudgetMB(mb int) {
	if mb < engine.MinResultBudgetMB {
		mb = engine.MinResultBudgetMB
	}
	if mb > engine.MaxResultBudgetMB {
		mb = engine.MaxResultBudgetMB
	}
	c.mu.Lock()
	c.budgetMB = mb
	c.mu.Unlock()
}

// ResultBudgetMB 返回本运行态的结果预算（MB）。
func (c *Console) ResultBudgetMB() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.budgetMB
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
	c.running, c.canceling, c.result, c.err = false, false, res, errMsg
	c.progRows, c.progBytes = 0, 0
}

// Canceling 报告是否已经点过停止、正在等驱动结束。
func (c *Console) Canceling() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && c.canceling
}

// Progress 返回执行中已读取的行数与字节数。
func (c *Console) Progress() (rows int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progRows, c.progBytes
}

// Run 在后台协程用 sess 执行 query；无会话、已在执行或空串时返回 false。
func (c *Console) Run(sess engine.Session, query string) bool {
	return c.RunTimeout(sess, query, 0)
}

// RunTimeout 同 Run，但用 timeout 作为本次执行的超时（<=0 用默认 queryTimeout）；
// 到期后 context 取消，驱动随之中止语句，错误改写为「查询超时」。
func (c *Console) RunTimeout(sess engine.Session, query string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = queryTimeout
	}
	c.mu.Lock()
	if sess == nil || c.running || query == "" {
		c.mu.Unlock()
		return false
	}
	c.gen++
	gen, pageSize, budgetMB := c.gen, c.pageSize, c.budgetMB
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ctx = engine.WithPageSize(ctx, pageSize)
	ctx = engine.WithResultBudget(ctx, budgetMB)
	ctx = engine.WithProgress(ctx, c.reportProgress)
	c.cancel = cancel
	c.running, c.canceling, c.result, c.err = true, false, nil, ""
	c.progRows, c.progBytes, c.progAt = 0, 0, time.Time{}
	c.mu.Unlock()

	start := time.Now()
	go func() {
		res, err := sess.RunStatement(ctx, query)
		ctxErr := ctx.Err()
		cancel()
		if err != nil && ctxErr == context.DeadlineExceeded {
			err = &TimeoutError{Timeout: timeout}
		}
		dur := time.Since(start)
		var done func(*engine.QueryResult, string, time.Duration)
		c.mu.Lock()
		if gen == c.gen {
			c.dur = dur
			c.running, c.canceling, c.cancel = false, false, nil
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

// TimeoutError 是一次执行超过连接配置的查询超时；错误文案给 UI 直接展示。
type TimeoutError struct{ Timeout time.Duration }

func (e *TimeoutError) Error() string {
	return locales.AppConsoleTimeout(e.Timeout.String())
}

// Cancel 取消当前执行：先立刻把状态标成「正在停止」并通知 UI，再取消 context。
func (c *Console) Cancel() {
	c.mu.Lock()
	if !c.running || c.cancel == nil {
		c.mu.Unlock()
		return
	}
	c.canceling = true
	cancel := c.cancel
	pub := c.publish
	c.mu.Unlock()
	pub(func() {})
	cancel()
}

// reportProgress 由驱动在扫描行时调用；节流后通知 UI，避免每行都重绘。
func (c *Console) reportProgress(p engine.QueryProgress) {
	now := time.Now()
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	if p.Rows == c.progRows && p.Bytes == c.progBytes {
		c.mu.Unlock()
		return
	}
	if p.Rows-c.progRows < 50 && now.Sub(c.progAt) < 200*time.Millisecond {
		c.mu.Unlock()
		return
	}
	c.progRows, c.progBytes, c.progAt = p.Rows, p.Bytes, now
	pub := c.publish
	c.mu.Unlock()
	pub(func() {})
}

// Generation 返回当前执行轮次；用于判断“取消后是否已经换了新查询”。
func (c *Console) Generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// AbortIf 在 gen 仍是当前轮次时强制结束：递增 gen，让卡在驱动里的旧查询结果作废。
// 返回 false 表示该轮次已经结束（或已经换了新查询），调用方不应再动连接。
func (c *Console) AbortIf(gen uint64, message string) bool {
	c.mu.Lock()
	if c.gen != gen || !c.running {
		c.mu.Unlock()
		return false
	}
	c.gen++
	c.running, c.canceling, c.cancel = false, false, nil
	c.result, c.err = nil, message
	c.dur, c.progRows, c.progBytes = 0, 0, 0
	pub := c.publish
	c.mu.Unlock()
	pub(func() {})
	return true
}
