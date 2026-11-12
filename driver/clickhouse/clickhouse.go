// Package clickhouse 提供 ClickHouse 数据源（只读优先：查询/对象树/列目录/流式导出）。
package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"db-g/engine"

	// clickhouse-go 固定在 v2.36.0：v2.37 重写的 HTTP 实现会查询 displayName()，
	// v2.38+ 还会无条件发送 client_protocol_version；ClickHouse 22.3 等老服务器
	// 分别报 UNKNOWN_FUNCTION / UNKNOWN_SETTING。升级前先确认目标服务器支持。
	_ "github.com/ClickHouse/clickhouse-go/v2"
)

const (
	rowLimit     = 500
	maxCellBytes = 1 << 20
)

func init() { engine.Register("clickhouse", openSession) }

type session struct {
	dsn       string
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
	schema    string // 期望的当前库（UseSchema 设置）
	applied   string // 已应用到连接池的库
}

// schemeFor 把连接协议换成 DSN 的 scheme：clickhouse-go 靠它决定走 native 还是
// HTTP 协议（见 clickhouse_options.go 的 ParseDSN），端口填错就握手失败。
func schemeFor(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http":
		return "http"
	case "https":
		return "https"
	}
	return "clickhouse"
}

func openSession(p engine.Profile) (engine.Session, error) {
	dsn := p.DSN
	if dsn == "" {
		scheme := schemeFor(p.Protocol)
		u := url.URL{Scheme: scheme, Host: fmt.Sprintf("%s:%d", p.Host, p.Port)}
		if p.User != "" {
			if p.Password != "" {
				u.User = url.UserPassword(p.User, p.Password)
			} else {
				u.User = url.User(p.User)
			}
		}
		u.Path = "/" + p.Database
		params, err := engine.Params(p.Params)
		if err != nil {
			return nil, fmt.Errorf("clickhouse: %w", err)
		}
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		// https 的 scheme 要求 DSN 里有 secure=true，否则 clickhouse-go 拒绝解析。
		if scheme == "https" {
			q.Set("secure", "true")
		}
		if len(q) > 0 {
			u.RawQuery = q.Encode()
		}
		dsn = u.String()
	}
	return &session{dsn: dsn}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("clickhouse", s.dsn)
	if err != nil {
		return err
	}
	// USE 只作用于一条连接；限制为 1 条，切换库后连接池保持一致。
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("clickhouse: %w", err)
	}
	s.db = db
	return nil
}

func (s *session) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *session) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapFetchMore | engine.CapObjectDDL | engine.CapExplain
}

// ServerInfo 返回当前库名与 ClickHouse 版本。
func (s *session) ServerInfo(ctx context.Context) (engine.ServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return engine.ServerInfo{}, errors.New("clickhouse: session not open")
	}
	var version, database string
	if err := s.db.QueryRowContext(ctx, "SELECT version(), currentDatabase()").Scan(&version, &database); err != nil {
		return engine.ServerInfo{}, err
	}
	return engine.ServerInfo{Database: database, Version: version}, nil
}

func (s *session) Cancel() bool {
	c := s.curCancel.Load()
	if c == nil {
		return false
	}
	(*c)()
	return true
}

// Explain 返回 ClickHouse 的 JSON 计划；暂不执行 EXPLAIN ANALYZE。
func (s *session) Explain(ctx context.Context, query string, opts engine.ExplainOptions) (*engine.QueryResult, error) {
	if opts.Analyze {
		return nil, errors.New("clickhouse 暂不支持 EXPLAIN ANALYZE")
	}
	return s.RunStatement(ctx, "EXPLAIN json=1 "+query)
}

func (s *session) RunStatement(parent context.Context, query string) (*engine.QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	ctx, cancel := context.WithCancel(parent)
	s.curCancel.Store(&cancel)
	defer func() {
		cancel()
		s.curCancel.Store(nil)
	}()
	if err := s.applySchemaLocked(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if res, eerr := s.db.ExecContext(ctx, query); eerr == nil {
			n, _ := res.RowsAffected()
			return &engine.QueryResult{Affected: n}, nil
		}
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &engine.QueryResult{Columns: cols, IsQuery: true}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	total := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if len(out.Rows) == rowLimit {
			out.Truncated = true
			break
		}
		for i, v := range vals {
			switch t := v.(type) {
			case string:
				if len(t) > maxCellBytes {
					t = trimStr(t, maxCellBytes)
					vals[i] = t
					out.Truncated = true
				}
				total += len(t)
			case []byte:
				if len(t) > maxCellBytes {
					t = trimBytes(t, maxCellBytes)
					vals[i] = append([]byte(nil), t...)
					out.Truncated = true
				}
				total += len(t)
			}
		}
		out.Rows = append(out.Rows, append([]any(nil), vals...))
		if total > engine.ResultBudgetBytes() {
			out.Truncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func trimStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}

func trimBytes(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	for max > 0 && b[max]&0xC0 == 0x80 {
		max--
	}
	return b[:max]
}

// Schemas 列出可切换的库。
func (s *session) Schemas(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM system.databases WHERE name NOT IN ('system','INFORMATION_SCHEMA','information_schema') ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// applySchemaLocked 把期望的库应用到连接（连接池只有一条连接）。
func (s *session) applySchemaLocked(ctx context.Context) error {
	if s.db == nil || s.schema == "" || s.schema == s.applied {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "USE "+quoteIdent(s.schema)); err != nil {
		return err
	}
	s.applied = s.schema
	return nil
}

// UseSchema 切换当前库。
func (s *session) UseSchema(ctx context.Context, schema string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("clickhouse: session not open")
	}
	s.schema = schema
	return s.applySchemaLocked(ctx)
}

// ActiveSchema 返回当前库；没有时返回空串。
func (s *session) ActiveSchema(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", errors.New("clickhouse: session not open")
	}
	if s.schema != "" {
		return s.schema, nil
	}
	return s.currentDatabaseLocked(ctx)
}

func (s *session) currentDatabaseLocked(ctx context.Context) (string, error) {
	var name sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT currentDatabase()`).Scan(&name); err != nil {
		return "", err
	}
	if !name.Valid {
		return "", nil
	}
	return name.String, nil
}

// schemaLocked 解析要用的库：显式传入优先，其次当前库/连接默认库。
func (s *session) schemaLocked(ctx context.Context, database string) (string, error) {
	if database != "" {
		return database, nil
	}
	if s.schema != "" {
		return s.schema, nil
	}
	return s.currentDatabaseLocked(ctx)
}

// clickhouseObjectClause 返回某类对象的 engine 过滤条件。
func clickhouseObjectClause(kind engine.ObjectKind) (string, error) {
	switch kind {
	case engine.ObjectTable:
		return "engine NOT IN ('View','MaterializedView')", nil
	case engine.ObjectView:
		return "engine IN ('View','MaterializedView')", nil
	case engine.ObjectIndex:
		return "", nil // ClickHouse 无索引对象
	default:
		return "", fmt.Errorf("clickhouse: unknown object kind %d", kind)
	}
}

// ObjectCounts 只查 system.tables 的计数，供大库树分组标题使用。
func (s *session) ObjectCounts(ctx context.Context, database string) (map[engine.ObjectKind]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	database, err := s.schemaLocked(ctx, database)
	if err != nil {
		return nil, err
	}
	q := `SELECT countIf(engine NOT IN ('View','MaterializedView')),
	             countIf(engine IN ('View','MaterializedView'))
	      FROM system.tables WHERE is_temporary = 0`
	var args []any
	if database != "" {
		q += ` AND database = ?`
		args = append(args, database)
	} else {
		q += ` AND database = currentDatabase()`
	}
	var tables, views uint64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&tables, &views); err != nil {
		return nil, err
	}
	return map[engine.ObjectKind]int{
		engine.ObjectTable: int(tables),
		engine.ObjectView:  int(views),
		engine.ObjectIndex: 0,
	}, nil
}

// ObjectsPage 按页列出某库的表/视图；limit <= 0 表示不分页。
func (s *session) ObjectsPage(ctx context.Context, database string, kind engine.ObjectKind, offset, limit int) ([]engine.DBObject, error) {
	clause, err := clickhouseObjectClause(kind)
	if err != nil {
		return nil, err
	}
	if kind == engine.ObjectIndex {
		return []engine.DBObject{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	database, err = s.schemaLocked(ctx, database)
	if err != nil {
		return nil, err
	}
	q := `SELECT name FROM system.tables WHERE is_temporary = 0 AND ` + clause
	var args []any
	if database != "" {
		q += ` AND database = ?`
		args = append(args, database)
	} else {
		q += ` AND database = currentDatabase()`
	}
	q += ` ORDER BY name`
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		q += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.DBObject
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, engine.DBObject{Kind: kind, Name: name, Schema: database})
	}
	return out, rows.Err()
}

// ObjectsIn 列出某库的表/视图。
func (s *session) ObjectsIn(ctx context.Context, database string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	return s.ObjectsPage(ctx, database, kind, 0, 0)
}

// ObjectDDL 用 SHOW CREATE TABLE 取建表语句。
func (s *session) ObjectDDL(ctx context.Context, kind engine.ObjectKind, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectDDLLocked(ctx, "", kind, name)
}

// ObjectDDLIn 按库取 DDL。
func (s *session) ObjectDDLIn(ctx context.Context, schema string, kind engine.ObjectKind, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectDDLLocked(ctx, schema, kind, name)
}

func (s *session) objectDDLLocked(ctx context.Context, schema string, kind engine.ObjectKind, name string) (string, error) {
	if s.db == nil {
		return "", errors.New("clickhouse: session not open")
	}
	if kind != engine.ObjectTable && kind != engine.ObjectView {
		return "", errors.New("clickhouse: 该对象不支持查看 DDL")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return "", err
	}
	qualified := quoteIdent(name)
	if schema != "" {
		qualified = quoteIdent(schema) + "." + qualified
	}
	q := "SHOW CREATE TABLE " + qualified
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		return "", fmt.Errorf("未找到 %s 的 DDL", name)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return "", err
	}
	for i := len(vals) - 1; i >= 0; i-- {
		switch t := vals[i].(type) {
		case string:
			return t, nil
		case []byte:
			return string(t), nil
		}
	}
	return "", fmt.Errorf("未找到 %s 的 DDL", name)
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	return s.ObjectsPage(ctx, "", kind, 0, 0)
}

func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.columnsLocked(ctx, "", table)
}

// ColumnsIn 按库列出列；多库补全走这里。
func (s *session) ColumnsIn(ctx context.Context, schema, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.columnsLocked(ctx, schema, table)
}

func (s *session) columnsLocked(ctx context.Context, schema, table string) ([]engine.Column, error) {
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	database, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	const q = `SELECT name, type FROM system.columns WHERE database = ? AND table = ? ORDER BY position`
	rows, err := s.db.QueryContext(ctx, q, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Column
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		out = append(out, engine.Column{Name: name, Type: typ})
	}
	return out, rows.Err()
}

func (s *session) Stream(ctx context.Context, query string, onColumns func([]string) error, onRow func([]any) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("clickhouse: session not open")
	}
	if err := s.applySchemaLocked(ctx); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if onColumns != nil {
		if err := onColumns(cols); err != nil {
			return err
		}
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = append([]byte(nil), b...)
			}
		}
		if err := onRow(vals); err != nil {
			return err
		}
	}
	return rows.Err()
}
