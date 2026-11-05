// Package clickhouse 提供 ClickHouse 数据源（只读优先：查询/对象树/列目录/流式导出）。
package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"

	"db-g/engine"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() { engine.Register("clickhouse", openSession) }

type session struct {
	dsn       string
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	dsn := p.DSN
	if dsn == "" {
		u := url.URL{Scheme: "clickhouse", Host: fmt.Sprintf("%s:%d", p.Host, p.Port)}
		if p.User != "" {
			if p.Password != "" {
				u.User = url.UserPassword(p.User, p.Password)
			} else {
				u.User = url.User(p.User)
			}
		}
		u.Path = "/" + p.Database
		dsn = u.String()
	}
	return &session{dsn: dsn}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("clickhouse", s.dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(2)
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

// Caps：只读数据源（无事务/无 DDL）。
func (s *session) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapFetchMore
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
		return nil, errors.New("clickhouse: session not open")
	}
	ctx, cancel := context.WithCancel(parent)
	s.curCancel.Store(&cancel)
	defer func() {
		cancel()
		s.curCancel.Store(nil)
	}()
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

func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	var q string
	switch kind {
	case engine.ObjectTable:
		q = `SELECT name FROM system.tables WHERE database = currentDatabase() AND engine NOT IN ('View','MaterializedView') ORDER BY name`
	case engine.ObjectView:
		q = `SELECT name FROM system.tables WHERE database = currentDatabase() AND engine IN ('View','MaterializedView') ORDER BY name`
	default:
		return []engine.DBObject{}, nil // ClickHouse 无索引对象
	}
	rows, err := s.db.QueryContext(ctx, q)
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
		out = append(out, engine.DBObject{Kind: kind, Name: name})
	}
	return out, rows.Err()
}

func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("clickhouse: session not open")
	}
	const q = `SELECT name, type FROM system.columns WHERE database = currentDatabase() AND table = ? ORDER BY position`
	rows, err := s.db.QueryContext(ctx, q, table)
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
