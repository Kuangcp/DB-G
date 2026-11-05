package engine

import (
	"context"
	"fmt"
	"sync"
)

// Profile 描述一个数据源连接的静态配置。
type Profile struct {
	Kind     string // "sqlite" | "mysql" | "postgres"
	Name     string // 展示名
	DSN      string // sqlite：文件路径或 ":memory:"
	Host     string // mysql：主机
	Port     int    // mysql：端口
	User     string // mysql：用户
	Password string // mysql：密码
	Database string // mysql：数据库（可空）
}

// Session 是协议无关的数据源会话契约；实现由 driver 包提供。
type Session interface {
	Open(ctx context.Context) error
	Close() error
	RunStatement(ctx context.Context, sql string) (*QueryResult, error)
	Cancel() bool
	Caps() Capabilities
}

// Column 是一个表的列元数据（列目录用）。
type Column struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
}

// ColumnLister 是可选能力接口：按表名列出一个表的列。
// UI 经类型断言与 CapColumns 能力位判定后使用；Session 接口本身不要求实现它。
type ColumnLister interface {
	Columns(ctx context.Context, table string) ([]Column, error)
}

// Tx 是一次显式事务：Exec 返回影响行数，Commit/Rollback 结束事务。
type Tx interface {
	Exec(ctx context.Context, query string, args ...any) (int64, error)
	Commit() error
	Rollback() error
}

// Transactional 是可选能力接口：会话能否开显式事务（写操作提交用）。
type Transactional interface {
	Begin(ctx context.Context) (Tx, error)
}

// ObjectDDLer 是可选能力接口：返回对象的 DDL（建表/建视图语句）。
type ObjectDDLer interface {
	ObjectDDL(ctx context.Context, kind ObjectKind, name string) (string, error)
}

// RowStreamer 是可选能力接口：把查询结果逐行流式交给调用方（全量导出用）。
// onColumns 在开始前给出列名；onRow 每行调用一次，返回错误即中止。
type RowStreamer interface {
	Stream(ctx context.Context, query string, onColumns func([]string) error, onRow func([]any) error) error
}

// Factory 创建未打开的会话。
type Factory func(p Profile) (Session, error)

var (
	regMu     sync.RWMutex
	factories = map[string]Factory{}
)

// Register 注册数据源工厂，由 driver 包 init() 调用；重复注册 panic。
func Register(kind string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := factories[kind]; dup {
		panic("engine: duplicate Register " + kind)
	}
	factories[kind] = f
}

// Open 按 Profile.Kind 创建未打开的会话。
func Open(p Profile) (Session, error) {
	regMu.RLock()
	f, ok := factories[p.Kind]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("engine: unknown data source kind %q", p.Kind)
	}
	return f(p)
}
