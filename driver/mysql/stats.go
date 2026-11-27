package mysql

import (
	"context"
	"database/sql"
	"strconv"

	"db-g/engine"
)

// ObjectStats 返回当前库中表的统计；行数取 information_schema 的估算值。
func (s *session) ObjectStats(ctx context.Context, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, "", kind, name)
}

// ObjectStatsIn 按库取统计：对象树跨库时不会被当前库带偏。
func (s *session) ObjectStatsIn(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, schema, kind, name)
}

func (s *session) objectStatsLocked(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	if s.db == nil {
		return nil, engine.NewUserError(engine.ErrSessionClosed, "")
	}
	if kind != engine.ObjectTable {
		return nil, engine.NewUserError(engine.ErrStatsTableOnly, "")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	stats := &engine.ObjectStats{}
	litSchema, litName := mysqlLiteral(schema), mysqlLiteral(name)
	stats.ExactRowsSQL = "SELECT COUNT(*) FROM " + quoteIdent(schema) + "." + quoteIdent(name)

	metaSQL := `SELECT t.TABLE_ROWS, t.DATA_LENGTH, t.INDEX_LENGTH, t.ENGINE, t.TABLE_COLLATION,
       t.AUTO_INCREMENT, t.ROW_FORMAT, t.TABLE_COMMENT, t.CREATE_TIME, t.UPDATE_TIME,
       (SELECT COUNT(*) FROM information_schema.COLUMNS c
         WHERE c.TABLE_SCHEMA = t.TABLE_SCHEMA AND c.TABLE_NAME = t.TABLE_NAME)
FROM information_schema.TABLES t
WHERE t.TABLE_SCHEMA = ` + litSchema + ` AND t.TABLE_NAME = ` + litName
	var (
		tableRows, dataLen, indexLen, autoInc, columns sql.NullInt64
		engineName, collation, rowFormat, comment      sql.NullString
		created, updated                               sql.NullTime
	)
	if err := s.db.QueryRowContext(ctx, metaSQL).Scan(
		&tableRows, &dataLen, &indexLen, &engineName, &collation,
		&autoInc, &rowFormat, &comment, &created, &updated, &columns,
	); err != nil {
		return nil, err
	}
	stats.SQL = append(stats.SQL, metaSQL)
	if columns.Valid {
		n := columns.Int64
		stats.Columns = &n
	}
	// TABLE_ROWS 是 InnoDB 的估算值；不再对大表执行 COUNT(*)。
	if tableRows.Valid {
		n := tableRows.Int64
		stats.Rows = &n
	}
	if dataLen.Valid {
		n := dataLen.Int64
		stats.DataBytes = &n
	}
	if indexLen.Valid {
		n := indexLen.Int64
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
	addTime := func(key string, v sql.NullTime) {
		if v.Valid {
			stats.Extra = append(stats.Extra, engine.ObjectStat{Key: key, Value: v.Time.Format("2006-01-02 15:04:05")})
		}
	}
	addStr("statMySQLEngine", engineName)
	addStr("statMySQLCollation", collation)
	addInt("statMySQLAutoIncrement", autoInc)
	addStr("statMySQLRowFormat", rowFormat)
	addStr("statMySQLComment", comment)
	addTime("statMySQLCreateTime", created)
	addTime("statMySQLUpdateTime", updated)
	return stats, nil
}

// mysqlLiteral 把字符串包成单引号字面量；统计弹窗展示的 SQL 可直接复制执行。
func mysqlLiteral(s string) string { return engine.QuoteString("mysql", s) }
