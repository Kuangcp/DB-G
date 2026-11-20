package engine

import "context"

// ObjectStat 是数据源特有的一条统计（键由驱动定义，UI 负责本地化已知键）。
type ObjectStat struct {
	Key   string
	Value string
}

// ObjectStats 是一个对象的统计信息。未知/不支持的项为 nil；
// SQL 是取得这些统计实际执行的语句，按执行顺序保存，供弹窗展示与复制。
type ObjectStats struct {
	Rows       *int64
	Columns    *int64
	DataBytes  *int64
	IndexBytes *int64
	Extra      []ObjectStat
	SQL        []string
	// ExactRowsSQL 是精确行数语句：弹窗不执行，只随 SQL 面板展示，供用户自行复制执行。
	ExactRowsSQL string
}

// ObjectStatter 是可选能力接口：返回表对象的统计信息（行数/列数/占用空间等）。
type ObjectStatter interface {
	ObjectStats(ctx context.Context, kind ObjectKind, name string) (*ObjectStats, error)
}

// SchemaObjectStatter 是可选能力接口：按对象所在 schema/库取统计。
type SchemaObjectStatter interface {
	ObjectStatsIn(ctx context.Context, schema string, kind ObjectKind, name string) (*ObjectStats, error)
}
