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
	Protocol string // clickhouse：连接协议，"native"（默认）/ "http" / "https"
	Params   string // 额外连接参数，key=value，见 Params
}

// Session 是协议无关的数据源会话契约；实现由 driver 包提供。
//
// 并发契约：同一 Session 上的 RunStatement 由驱动内部串行化；调用方可以并发提交，
// 但会排队。取消只通过传给 RunStatement 的 context 完成，驱动不得依赖会话级取消槽。
// Close 时若仍有语句在跑，驱动应尽量让 Close 返回而不是永久阻塞。
type Session interface {
	Open(ctx context.Context) error
	Close() error
	RunStatement(ctx context.Context, sql string) (*QueryResult, error)
	Caps() Capabilities
}

// ServerInfo 是已连接数据源的运行时信息（结果区底栏展示用）。
type ServerInfo struct {
	Database string // 当前数据库/文件
	Version  string // 数据源版本
}

// ServerInfoer 是可选能力接口：返回当前数据库名与服务器版本。
// 打开连接后 UI 异步查询一次，失败只把版本留空，不影响会话。
type ServerInfoer interface {
	ServerInfo(ctx context.Context) (ServerInfo, error)
}

// Column 是一个表的列元数据（列目录用）。
type Column struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
	Comment    string // 数据库里的列备注；没有则为空
}

// ColumnLister 是可选能力接口：按表名列出一个表的列。
// UI 经类型断言与 CapColumns 能力位判定后使用；Session 接口本身不要求实现它。
type ColumnLister interface {
	Columns(ctx context.Context, table string) ([]Column, error)
}

// SchemaColumnLister 是可选能力接口：按 schema/库 + 表名列出列。
// 多 schema 数据源必须实现它，否则未选默认库时无法取列（MySQL 的 "未选择数据库"）。
type SchemaColumnLister interface {
	ColumnsIn(ctx context.Context, schema, table string) ([]Column, error)
}

// Index 是一个索引的元数据（表视图结构页用）。
type Index struct {
	Name      string
	Columns   []string // 按索引定义顺序；含前缀长度/排序方向
	Unique    bool
	Primary   bool
	Type      string // btree/hash/gin/…；没有类型概念时为空
	Predicate string // 部分索引的 WHERE 条件；没有则为空
}

// IndexLister 是可选能力接口：列出一个表的索引。
// UI 经类型断言与 CapIndexes 能力位判定后使用；Session 接口本身不要求实现它。
type IndexLister interface {
	Indexes(ctx context.Context, table string) ([]Index, error)
}

// SchemaIndexLister 是可选能力接口：按 schema/库 + 表名列出索引。
type SchemaIndexLister interface {
	IndexesIn(ctx context.Context, schema, table string) ([]Index, error)
}

// ForeignKey 是一个列的外键指向（供 AI 检索时做一跳扩展）。
type ForeignKey struct {
	Column    string
	RefSchema string
	RefTable  string
	RefColumn string
}

// ForeignKeyer 是可选能力接口：列出一个表的外键。
// 只在 AI 组装上下文时按需调用，不进入普通补全/对象树的热路径。
type ForeignKeyer interface {
	ForeignKeys(ctx context.Context, schema, table string) ([]ForeignKey, error)
}

// SchemaSwitcher 是可选能力接口：切换会话的当前 schema/库。
// MySQL 用 USE <库>，PostgreSQL 用 SET search_path，ClickHouse 用 USE <库>。
type SchemaSwitcher interface {
	UseSchema(ctx context.Context, schema string) error
	ActiveSchema(ctx context.Context) (string, error)
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

// SchemaObjectDDLer 是可选能力接口：按对象所在 schema/库取 DDL。
// 多 schema 数据源用它，避免把其他库的对象拿到当前库去查。
type SchemaObjectDDLer interface {
	ObjectDDLIn(ctx context.Context, schema string, kind ObjectKind, name string) (string, error)
}

// ExplainOptions 控制执行计划的生成方式。
type ExplainOptions struct {
	Analyze bool // 真实执行查询并收集实际耗时/行数（只允许 SELECT）
	Buffers bool // PostgreSQL：ANALYZE 时附带 buffer 统计
}

// PlanExplainer 是可选能力接口：返回数据源原生的 EXPLAIN 结果。
// 结果由 ai/plan 按 kind 解析成统一计划树；解析失败时 UI 回退显示原始结果。
type PlanExplainer interface {
	Explain(ctx context.Context, sql string, opts ExplainOptions) (*QueryResult, error)
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

// Registered 报告某种数据源的工厂是否已注册：程序没有链接对应的 driver 包时
// 它就是 false，Open 会报 unknown kind。
func Registered(kind string) bool {
	regMu.RLock()
	defer regMu.RUnlock()
	_, ok := factories[kind]
	return ok
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
