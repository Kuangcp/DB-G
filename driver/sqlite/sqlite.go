package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"db-g/engine"

	_ "modernc.org/sqlite"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() {
	engine.Register("sqlite", openSession)
}

type session struct {
	path      string
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	return &session{path: p.DSN}, nil
}

func (s *session) Open(ctx context.Context) error {
	if s.path != ":memory:" {
		if _, err := os.Stat(s.path); err != nil {
			return fmt.Errorf("sqlite: %w", err)
		}
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return err
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

func (s *session) Caps() engine.Capabilities { return 0 }

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
		return nil, errors.New("sqlite: session not open")
	}
	ctx, cancel := context.WithCancel(parent)
	s.curCancel.Store(&cancel)
	defer func() {
		cancel()
		s.curCancel.Store(nil)
	}()
	if isQueryLike(query) {
		return s.query(ctx, query)
	}
	res, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		n = 0
	}
	return &engine.QueryResult{Affected: n}, nil
}

func isQueryLike(q string) bool {
	q = strings.TrimSpace(q)
	if i := strings.IndexFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n'
	}); i >= 0 {
		q = q[:i]
	}
	switch strings.ToUpper(q) {
	case "SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES":
		return true
	}
	return false
}

func (s *session) query(ctx context.Context, query string) (*engine.QueryResult, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &engine.QueryResult{Columns: cols, IsQuery: true}
	total := 0
	for rows.Next() {
		if len(out.Rows) == rowLimit {
			out.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
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
		out.Rows = append(out.Rows, vals)
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
