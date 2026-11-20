package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"db-g/engine"
)

// ObjectStats 返回表的估算行数、列数与占用空间；尺寸依赖 dbstat 虚拟表，
// 编译时未开启时只跳过尺寸，其余统计照常返回。
func (s *session) ObjectStats(ctx context.Context, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	return s.ObjectStatsIn(ctx, "", kind, name)
}

// ObjectStatsIn 与 ObjectStats 相同；SQLite 没有 schema 概念，忽略 schema。
func (s *session) ObjectStatsIn(ctx context.Context, _ string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("sqlite: session not open")
	}
	if kind != engine.ObjectTable {
		return nil, errors.New("sqlite: 仅表支持查看统计")
	}
	stats := &engine.ObjectStats{}
	lit := quoteLiteral(name)
	stats.ExactRowsSQL = "SELECT COUNT(*) FROM " + quoteIdent(name)

	// 行数只用 sqlite_stat1 的估算值（最后一次 ANALYZE 的结果），不扫表。
	rowsSQL := "SELECT stat FROM sqlite_stat1 WHERE tbl = " + lit + " LIMIT 1"
	stats.SQL = append(stats.SQL, rowsSQL)
	var stat sql.NullString
	if err := s.db.QueryRowContext(ctx, rowsSQL).Scan(&stat); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, err
		}
		stats.Extra = append(stats.Extra, engine.ObjectStat{Key: "statSQLiteNoAnalyze", Value: ""})
	} else if n, ok := sqliteRowEstimate(stat.String); ok {
		stats.Rows = &n
	}

	metaSQL := "SELECT (SELECT page_size FROM pragma_page_size), " +
		"(SELECT page_count FROM pragma_page_count), " +
		"(SELECT freelist_count FROM pragma_freelist_count), " +
		"(SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = " + lit + "), " +
		"(SELECT COUNT(*) FROM pragma_table_info(" + lit + "))"
	var pageSize, pageCount, freelist, rootPage, columns sql.NullInt64
	if err := s.db.QueryRowContext(ctx, metaSQL).Scan(&pageSize, &pageCount, &freelist, &rootPage, &columns); err != nil {
		return nil, err
	}
	stats.SQL = append(stats.SQL, metaSQL)
	if columns.Valid {
		n := columns.Int64
		stats.Columns = &n
	}
	addStat := func(key string, v sql.NullInt64) {
		if v.Valid {
			stats.Extra = append(stats.Extra, engine.ObjectStat{Key: key, Value: strconv.FormatInt(v.Int64, 10)})
		}
	}
	addStat("statSQLitePageSize", pageSize)
	addStat("statSQLitePageCount", pageCount)
	addStat("statSQLiteFreelist", freelist)
	addStat("statSQLiteRootPage", rootPage)

	dataSQL := "SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name = " + lit
	var dataBytes int64
	if err := s.db.QueryRowContext(ctx, dataSQL).Scan(&dataBytes); err != nil {
		stats.Extra = append(stats.Extra, engine.ObjectStat{Key: "statError", Value: err.Error()})
	} else {
		stats.DataBytes = &dataBytes
		stats.SQL = append(stats.SQL, dataSQL)
	}

	indexSQL := "SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name IN " +
		"(SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = " + lit + ")"
	var indexBytes int64
	if err := s.db.QueryRowContext(ctx, indexSQL).Scan(&indexBytes); err != nil {
		stats.Extra = append(stats.Extra, engine.ObjectStat{Key: "statError", Value: err.Error()})
	} else {
		stats.IndexBytes = &indexBytes
		stats.SQL = append(stats.SQL, indexSQL)
	}
	return stats, nil
}

// sqliteRowEstimate 从 sqlite_stat1.stat 的第一个数字取出估算行数，例如 "1000 2 1"。
func sqliteRowEstimate(stat string) (int64, bool) {
	fields := strings.Fields(stat)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// quoteLiteral 把字符串包成单引号字面量（统计弹窗里展示的 SQL 可直接复制执行）。
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
