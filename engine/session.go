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
