package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"db-g/engine"
)

// ObjectStats 返回当前 schema 中表的统计；行数取 pg_class.reltuples 估算值。
func (s *session) ObjectStats(ctx context.Context, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, "", kind, name)
}

// ObjectStatsIn 按 schema 取统计：对象树跨 schema 时不会被当前 schema 带偏。
func (s *session) ObjectStatsIn(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectStatsLocked(ctx, schema, kind, name)
}

func (s *session) objectStatsLocked(ctx context.Context, schema string, kind engine.ObjectKind, name string) (*engine.ObjectStats, error) {
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	if kind != engine.ObjectTable {
		return nil, errors.New("postgres: 仅表支持查看统计")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	if schema == "" {
		return nil, errors.New("postgres: 未选择 schema")
	}
	stats := &engine.ObjectStats{}
	rel := quoteIdent(schema) + "." + quoteIdent(name)
	litSchema, litName := pgLiteral(schema), pgLiteral(name)
	litRel := pgLiteral(rel)
	stats.ExactRowsSQL = "SELECT count(*) FROM " + rel

	metaSQL := `SELECT
       (SELECT count(*) FROM information_schema.columns
         WHERE table_schema = ` + litSchema + ` AND table_name = ` + litName + `),
       pg_table_size(` + litRel + `),
       pg_indexes_size(` + litRel + `),
       pg_total_relation_size(` + litRel + `),
       (SELECT c.reltuples::bigint FROM pg_class c
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = ` + litSchema + ` AND c.relname = ` + litName + `),
       s.n_live_tup, s.n_dead_tup,
       s.last_vacuum, s.last_autovacuum, s.last_analyze, s.last_autoanalyze
FROM (SELECT 1) AS one
LEFT JOIN pg_stat_user_tables s
  ON s.schemaname = ` + litSchema + ` AND s.relname = ` + litName
	var (
		columns, dataBytes, indexBytes, totalBytes, relTuples, liveTuples, deadTuples sql.NullInt64
		lastVacuum, lastAutovacuum, lastAnalyze, lastAutoanalyze                      sql.NullTime
	)
	if err := s.db.QueryRowContext(ctx, metaSQL).Scan(
		&columns, &dataBytes, &indexBytes, &totalBytes, &relTuples, &liveTuples, &deadTuples,
		&lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze,
	); err != nil {
		return nil, err
	}
	stats.SQL = append(stats.SQL, metaSQL)
	if columns.Valid {
		n := columns.Int64
		stats.Columns = &n
	}
	// reltuples 是 ANALYZE/自动统计给的估算值；未统计过（-1）时退回 n_live_tup。
	if relTuples.Valid && relTuples.Int64 >= 0 {
		n := relTuples.Int64
		stats.Rows = &n
	} else if liveTuples.Valid {
		n := liveTuples.Int64
		stats.Rows = &n
	}
	if dataBytes.Valid {
		n := dataBytes.Int64
		stats.DataBytes = &n
	}
	if indexBytes.Valid {
		n := indexBytes.Int64
		stats.IndexBytes = &n
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
	addInt("statPostgresTotalBytes", totalBytes)
	addInt("statPostgresLiveTuples", liveTuples)
	addInt("statPostgresDeadTuples", deadTuples)
	addTime("statPostgresLastVacuum", lastVacuum)
	addTime("statPostgresLastAutovacuum", lastAutovacuum)
	addTime("statPostgresLastAnalyze", lastAnalyze)
	addTime("statPostgresLastAutoanalyze", lastAutoanalyze)
	return stats, nil
}

// pgLiteral 把字符串包成 PostgreSQL 单引号字面量（展示的 SQL 可直接复制执行）。
func pgLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
