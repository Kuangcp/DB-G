// Package postgres 提供 PostgreSQL 数据源（pgx stdlib 驱动）。
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"db-g/engine"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() { engine.Register("postgres", openSession) }

type session struct {
	dsn       string
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	dsn := p.DSN
	if dsn == "" {
		u := url.URL{Scheme: "postgres", Host: fmt.Sprintf("%s:%d", p.Host, p.Port)}
		if p.User != "" {
			if p.Password != "" {
				u.User = url.UserPassword(p.User, p.Password)
			} else {
				u.User = url.User(p.User)
			}
		}
		u.Path = "/" + p.Database
		q := url.Values{"sslmode": {"disable"}} // 默认不加密，用户可以用参数覆盖
		params, err := engine.Params(p.Params)
		if err != nil {
			return nil, fmt.Errorf("postgres: %w", err)
		}
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	return &session{dsn: dsn}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("pgx", s.dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("postgres: %w", err)
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
	return engine.CapObjects | engine.CapColumns | engine.CapFetchMore | engine.CapEditableResult | engine.CapObjectDDL
}

func (s *session) Cancel() bool {
	c := s.curCancel.Load()
	if c == nil {
		return false
	}
	(*c)()
	return true
}

func (s *session) RunStatement(parent context.Context, query string) (*engine.QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	ctx, cancel := context.WithCancel(parent)
	s.curCancel.Store(&cancel)
	defer func() {
		cancel()
		s.curCancel.Store(nil)
	}()
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		// 非查询语句（INSERT/UPDATE/DDL）走 Exec。
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
		if total > maxResultByte {
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

// Schemas 列出可切换的 schema（排除系统 schema）。
func (s *session) Schemas(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	const q = `SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog','information_schema')
		  AND schema_name NOT LIKE 'pg_%' ORDER BY 1`
	rows, err := s.db.QueryContext(ctx, q)
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

// ObjectsIn 列出某 schema 的表/视图/索引。
func (s *session) ObjectsIn(ctx context.Context, schema string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	var q string
	switch kind {
	case engine.ObjectTable:
		q = `SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY 1`
	case engine.ObjectView:
		q = `SELECT table_name FROM information_schema.views WHERE table_schema = $1 ORDER BY 1`
	case engine.ObjectIndex:
		q = `SELECT indexname FROM pg_indexes WHERE schemaname = $1 ORDER BY 1`
	default:
		return nil, fmt.Errorf("postgres: unknown object kind %d", kind)
	}
	rows, err := s.db.QueryContext(ctx, q, schema)
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
		out = append(out, engine.DBObject{Kind: kind, Name: name, Schema: schema})
	}
	return out, rows.Err()
}

// Objects 列出当前 schema 的对象（保持 ObjectLister 兼容）。
func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	schema, err := s.currentSchema(ctx)
	if err != nil {
		return nil, err
	}
	return s.ObjectsIn(ctx, schema, kind)
}

func (s *session) currentSchema(ctx context.Context) (string, error) {
	var schema string
	err := s.db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema)
	return schema, err
}

// ObjectDDL 尽力拼出建表语句（列/类型/非空/默认/主键）；视图用 pg_get_viewdef。
func (s *session) ObjectDDL(ctx context.Context, kind engine.ObjectKind, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", errors.New("postgres: session not open")
	}
	schema, err := s.currentSchema(ctx)
	if err != nil {
		return "", err
	}
	if kind == engine.ObjectView {
		var def string
		err := s.db.QueryRowContext(ctx,
			`SELECT 'CREATE OR REPLACE VIEW '||quote_ident($1)||'.'||quote_ident($2)||' AS '||pg_get_viewdef($1||'.'||$2, true)`,
			schema, name).Scan(&def)
		if err != nil {
			return "", err
		}
		return def + ";", nil
	}
	if kind != engine.ObjectTable {
		return "", errors.New("postgres: 该对象不支持查看 DDL")
	}
	const q = `
SELECT c.column_name, c.data_type, c.is_nullable, coalesce(c.column_default,''), coalesce(c.character_maximum_length,0),
       EXISTS (
         SELECT 1 FROM information_schema.table_constraints tc
         JOIN information_schema.key_column_usage k
           ON k.constraint_name = tc.constraint_name AND k.table_schema = tc.table_schema AND k.table_name = tc.table_name
         WHERE tc.constraint_type='PRIMARY KEY' AND tc.table_schema=$1 AND tc.table_name=$2 AND k.column_name=c.column_name)
FROM information_schema.columns c
WHERE c.table_schema=$1 AND c.table_name=$2 ORDER BY c.ordinal_position`
	rows, err := s.db.QueryContext(ctx, q, schema, name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	type col struct {
		name, typ, def string
		nullable       bool
		length         int64
		pk             bool
	}
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.typ, &c.nullable, &c.def, &c.length, &c.pk); err != nil {
			return "", err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("未找到 %s 的 DDL", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s.%s (\n", quoteIdent(schema), quoteIdent(name))
	var pks []string
	for i, c := range cols {
		typ := c.typ
		if c.length > 0 {
			typ = fmt.Sprintf("%s(%d)", typ, c.length)
		}
		fmt.Fprintf(&b, "  %s %s", quoteIdent(c.name), typ)
		if !c.nullable {
			b.WriteString(" NOT NULL")
		}
		if c.def != "" {
			b.WriteString(" DEFAULT " + c.def)
		}
		if c.pk {
			pks = append(pks, quoteIdent(c.name))
		}
		if i < len(cols)-1 || len(pks) > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	if len(pks) > 0 {
		fmt.Fprintf(&b, "  PRIMARY KEY (%s)\n", strings.Join(pks, ", "))
	}
	b.WriteString(");")
	return b.String(), nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	const q = `
SELECT c.column_name, c.data_type, c.is_nullable,
       EXISTS (
         SELECT 1 FROM information_schema.table_constraints tc
         JOIN information_schema.key_column_usage k
           ON k.constraint_name = tc.constraint_name
          AND k.table_schema = tc.table_schema
          AND k.table_name = tc.table_name
         WHERE tc.constraint_type = 'PRIMARY KEY'
           AND tc.table_schema = current_schema()
           AND tc.table_name = c.table_name
           AND k.column_name = c.column_name
       )
FROM information_schema.columns c
WHERE c.table_schema = current_schema() AND c.table_name = $1
ORDER BY c.ordinal_position`
	rows, err := s.db.QueryContext(ctx, q, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Column
	for rows.Next() {
		var name, typ, nullable string
		var pk bool
		if err := rows.Scan(&name, &typ, &nullable, &pk); err != nil {
			return nil, err
		}
		out = append(out, engine.Column{Name: name, Type: typ, Nullable: nullable == "YES", PrimaryKey: pk})
	}
	return out, rows.Err()
}

func (s *session) Stream(ctx context.Context, query string, onColumns func([]string) error, onRow func([]any) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("postgres: session not open")
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

type txWrap struct{ tx *sql.Tx }

func (t *txWrap) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
func (t *txWrap) Commit() error   { return t.tx.Commit() }
func (t *txWrap) Rollback() error { return t.tx.Rollback() }

func (s *session) Begin(ctx context.Context) (engine.Tx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("postgres: session not open")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &txWrap{tx: tx}, nil
}

var _ = strconv.Itoa
