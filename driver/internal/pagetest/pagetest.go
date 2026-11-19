// Package pagetest 提供只用于测试的 database/sql 驱动，模拟真实驱动的
// 「Rows.Close 会读完剩余结果集」行为（MySQL 的 skipRows、pgx 的
// ResultReader.Close、clickhouse-go 的 stream 排空）。
//
// 驱动按页读取后必须取消查询再 Close，否则大结果集会在 Close 里被整份读掉，
// 表现为 UI 行数停住、结果晚好几秒才出现。这里的 Probe 让驱动包的测试能
// 断言「Close 之前已经取消了 context」。
package pagetest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// Probe 记录假结果集被如何关闭。
type Probe struct {
	Total int // 结果集总行数

	drained       atomic.Int64 // 未取消时 Close 额外读掉的行数
	closeCanceled atomic.Bool  // Close 时 context 是否已取消
	closed        atomic.Bool
}

// Drained 返回 Close 排空读掉的行数（取消后应为 0）。
func (p *Probe) Drained() int64 { return p.drained.Load() }

// CloseCanceled 报告 Close 时查询 context 是否已取消。
func (p *Probe) CloseCanceled() bool { return p.closeCanceled.Load() }

// Open 返回一个连到假驱动的 *sql.DB；查询总是返回 total 行 int64 值。
func Open(total int) (*sql.DB, *Probe) {
	p := &Probe{Total: total}
	return sql.OpenDB(connector{p}), p
}

type connector struct{ p *Probe }

func (c connector) Connect(context.Context) (driver.Conn, error) { return conn{c.p}, nil }
func (c connector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("pagetest: use the connector")
}

type conn struct{ p *Probe }

func (conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("pagetest: not supported") }
func (conn) Close() error                        { return nil }
func (conn) Begin() (driver.Tx, error)           { return nil, errors.New("pagetest: not supported") }

func (c conn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &rows{p: c.p, ctx: ctx}, nil
}

type rows struct {
	p      *Probe
	ctx    context.Context
	next   int
	once   sync.Once
	closed bool
}

func (*rows) Columns() []string { return []string{"id"} }

func (r *rows) Next(dest []driver.Value) error {
	if r.next >= r.p.Total {
		return io.EOF
	}
	dest[0] = int64(r.next)
	r.next++
	return nil
}

// Close 复刻真实驱动：context 未取消时把剩余行全部读完再释放连接。
func (r *rows) Close() error {
	r.once.Do(func() {
		r.closed = true
		r.p.closed.Store(true)
		if r.ctx.Err() != nil {
			r.p.closeCanceled.Store(true)
			return
		}
		for r.next < r.p.Total {
			r.p.drained.Add(1)
			r.next++
		}
	})
	return nil
}
