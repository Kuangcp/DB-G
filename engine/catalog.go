package engine

import (
	"context"
	"errors"
)

// ErrUnsupported 表示会话没有实现对应的可选能力。
var ErrUnsupported = errors.New("engine: capability not supported")

// ObjectsOf 枚举对象：优先用 schema 感知实现，其次退回单 schema 实现。
func ObjectsOf(ctx context.Context, sess Session, schema string, kind ObjectKind) ([]DBObject, error) {
	if l, ok := sess.(SchemaObjectLister); ok {
		return l.ObjectsIn(ctx, schema, kind)
	}
	return nil, ErrUnsupported
}

// ColumnsOf 列出表列：优先 schema 感知实现。
func ColumnsOf(ctx context.Context, sess Session, schema, table string) ([]Column, error) {
	if l, ok := sess.(SchemaColumnLister); ok {
		return l.ColumnsIn(ctx, schema, table)
	}
	return nil, ErrUnsupported
}

// IndexesOf 列出表索引：优先 schema 感知实现。
func IndexesOf(ctx context.Context, sess Session, schema, table string) ([]Index, error) {
	if l, ok := sess.(SchemaIndexLister); ok {
		return l.IndexesIn(ctx, schema, table)
	}
	return nil, ErrUnsupported
}

// ObjectDDLOf 取对象 DDL：优先 schema 感知实现。
func ObjectDDLOf(ctx context.Context, sess Session, schema string, kind ObjectKind, name string) (string, error) {
	if l, ok := sess.(SchemaObjectDDLer); ok {
		return l.ObjectDDLIn(ctx, schema, kind, name)
	}
	return "", ErrUnsupported
}

// ObjectStatsOf 取对象统计：优先 schema 感知实现。
func ObjectStatsOf(ctx context.Context, sess Session, schema string, kind ObjectKind, name string) (*ObjectStats, error) {
	if l, ok := sess.(SchemaObjectStatter); ok {
		return l.ObjectStatsIn(ctx, schema, kind, name)
	}
	return nil, ErrUnsupported
}

// ObjectCountsOf 取各类型对象数量。
func ObjectCountsOf(ctx context.Context, sess Session, schema string) (map[ObjectKind]int, error) {
	if c, ok := sess.(ObjectCounter); ok {
		return c.ObjectCounts(ctx, schema)
	}
	return nil, ErrUnsupported
}

// ObjectsPageOf 分页枚举对象。
func ObjectsPageOf(ctx context.Context, sess Session, schema string, kind ObjectKind, offset, limit int) ([]DBObject, error) {
	if p, ok := sess.(PagedObjectLister); ok {
		return p.ObjectsPage(ctx, schema, kind, offset, limit)
	}
	return nil, ErrUnsupported
}
