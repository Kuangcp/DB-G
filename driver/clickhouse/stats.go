package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"db-g/engine"
)

// ObjectStats 返回当前库中表的统计。
func (s *session) ObjectStats(ctx context.Context, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, "", kind, name)
}

// ObjectStatsIn 按库取统计。
func (s *session) ObjectStatsIn(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, schema, kind, name)
}

func (s *session) objectStatsLocked(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	if kind != engine.ObjectTable {
		return nil, errors.New("clickhouse: 仅表支持查看统计")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	stats := &engine.ObjectStats{}
	qualified := quoteIdent(name)
	if schema != "" {
		qualified = quoteIdent(schema) + "." + qualified
	}
	litDB, litName := chLiteral(schema), chLiteral(name)

	rowsSQL := "SELECT toInt64(count()) FROM " + qualified
	var rows int64
	if err := s.db.QueryRowContext(ctx, rowsSQL).Scan(&rows); err != nil {
		return nil, err
	}
	stats.Rows = &rows
	stats.SQL = append(stats.SQL, rowsSQL)

	metaSQL := `SELECT engine, sorting_key, primary_key,
       toInt64(coalesce(total_rows, 0)), toInt64(coalesce(total_bytes, 0)),
       toInt64((SELECT count() FROM system.columns
                WHERE database = ` + litDB + ` AND table = ` + litName + `))
FROM system.tables t
WHERE database = ` + litDB + ` AND name = ` + litName
	var (
		engineName, sortingKey, primaryKey sql.NullString
		totalRows, totalBytes, columns     sql.NullInt64
	)
	if err := s.db.QueryRowContext(ctx, metaSQL).Scan(
		&engineName, &sortingKey, &primaryKey, &totalRows, &totalBytes, &columns,
	); err != nil {
		return nil, err
	}
	stats.SQL = append(stats.SQL, metaSQL)
	if columns.Valid {
		n := columns.Int64
		stats.Columns = &n
	}

	partsSQL := `SELECT toInt64(count()), toInt64(coalesce(sum(rows), 0)),
       toInt64(coalesce(sum(bytes_on_disk), 0)), toInt64(coalesce(sum(data_uncompressed_bytes), 0)),
       toInt64(coalesce(sum(primary_key_size), 0)), toInt64(coalesce(sum(primary_key_bytes_in_memory), 0))
FROM system.parts
WHERE active AND database = ` + litDB + ` AND table = ` + litName
	var parts, partRows, dataBytes, uncompressed, indexBytes, primaryKeyMemory sql.NullInt64
	if err := s.db.QueryRowContext(ctx, partsSQL).Scan(
		&parts, &partRows, &dataBytes, &uncompressed, &indexBytes, &primaryKeyMemory,
	); err != nil {
		return nil, err
	}
	stats.SQL = append(stats.SQL, partsSQL)
	if dataBytes.Valid {
		n := dataBytes.Int64
		stats.DataBytes = &n
	}
	if indexBytes.Valid {
		n := indexBytes.Int64
		stats.IndexBytes = &n
	}
	addStr := func(key string, v sql.NullString) {
		if v.Valid && v.String != "" {
			stats.Extra = append(stats.Extra, engine.ObjectStat{Key: key, Value: v.String})
		}
	}
	addInt := func(key string, v sql.NullInt64) {
		if v.Valid {
			stats.Extra = append(stats.Extra, engine.ObjectStat{Key: key, Value: strconv.FormatInt(v.Int64, 10)})
		}
	}
	addStr("statClickHouseEngine", engineName)
	addStr("statClickHouseSortingKey", sortingKey)
	addStr("statClickHousePrimaryKey", primaryKey)
	addInt("statClickHouseTotalRows", totalRows)
	addInt("statClickHouseTotalBytes", totalBytes)
	addInt("statClickHouseParts", parts)
	addInt("statClickHouseUncompressedBytes", uncompressed)
	addInt("statClickHousePrimaryKeyMemory", primaryKeyMemory)
	if dataBytes.Valid && dataBytes.Int64 > 0 && uncompressed.Valid {
		ratio := float64(uncompressed.Int64) / float64(dataBytes.Int64)
		stats.Extra = append(stats.Extra, engine.ObjectStat{
			Key: "statClickHouseCompressionRatio", Value: fmt.Sprintf("%.2fx", ratio),
		})
	}
	return stats, nil
}

// chLiteral 把字符串包成 ClickHouse 单引号字面量（展示的 SQL 可直接复制执行）。
func chLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}
