package engine

import "context"

// ObjectKind 是对象树中的条目类型。
type ObjectKind int

const (
	ObjectTable ObjectKind = iota
	ObjectView
	ObjectIndex
)

// DBObject 是对象树枚举出的一个条目。
type DBObject struct {
	Kind   ObjectKind
	Name   string
	Schema string // 多 schema 数据源的非默认 schema；空 = 默认/无 schema
}

// ObjectLister 是可选能力接口：数据源枚举自身对象（表/视图/索引）。
// UI 经类型断言与 CapObjects 能力位判定后使用。
type ObjectLister interface {
	Objects(ctx context.Context, kind ObjectKind) ([]DBObject, error)
}

// SchemaLister 是可选能力接口：数据源有多个 schema/库可切换（PG、MySQL、CK）。
// UI 先列 Schemas，再用 ObjectsIn 取某个 schema 的对象。
type SchemaLister interface {
	Schemas(ctx context.Context) ([]string, error)
	ObjectsIn(ctx context.Context, schema string, kind ObjectKind) ([]DBObject, error)
}
