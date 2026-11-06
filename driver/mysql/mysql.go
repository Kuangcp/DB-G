package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"db-g/engine"

	"github.com/go-sql-driver/mysql"
)

const (
	rowLimit      = 500
	maxCellBytes  = 1 << 20
	maxResultByte = 16 << 20
)

func init() {
	engine.Register("mysql", openSession)
}

type session struct {
	cfg       *mysql.Config
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
}

func openSession(p engine.Profile) (engine.Session, error) {
	cfg := mysql.NewConfig()
	cfg.User = p.User
	cfg.Passwd = p.Password
	cfg.Net = "tcp"
	addr := p.Host
	if addr == "" {
		return nil, errors.New("mysql: 主机不能为空")
	}
	if p.Port > 0 {
		addr += ":" + strconv.Itoa(p.Port)
	}
	cfg.Addr = addr
	cfg.DBName = p.Database
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.Loc = time.Local
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	return &session{cfg: cfg}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("mysql", s.cfg.FormatDSN())
	if err != nil {
		return err
	}
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

func (s *session) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapFetchMore | engine.CapObjectDDL | engine.CapEditableResult
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
		return nil, errors.New("mysql: session not open")
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
	case "SELECT", "WITH", "EXPLAIN", "SHOW", "DESCRIBE", "DESC", "VALUES":
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

// Schemas 列出可切换的库（排除系统库）。
func (s *session) Schemas(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	const q = `SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('mysql','information_schema','performance_schema','sys') ORDER BY 1`
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

// ObjectsIn 列出某库的表/视图/索引。
func (s *session) ObjectsIn(ctx context.Context, schema string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	if schema == "" {
		schema = s.cfg.DBName
	}
	if schema == "" {
		return nil, errors.New("mysql: 未选择数据库")
	}
	var q string
	switch kind {
	case engine.ObjectTable:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
	case engine.ObjectView:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'VIEW' ORDER BY TABLE_NAME`
	case engine.ObjectIndex:
		q = `SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? GROUP BY INDEX_NAME ORDER BY INDEX_NAME`
	default:
		return nil, fmt.Errorf("mysql: unknown object kind %d", kind)
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

func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	if s.cfg.DBName == "" {
		return nil, errors.New("mysql: 未选择数据库")
	}
	var q string
	switch kind {
	case engine.ObjectTable:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
	case engine.ObjectView:
		q = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'VIEW' ORDER BY TABLE_NAME`
	case engine.ObjectIndex:
		q = `SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? GROUP BY INDEX_NAME ORDER BY INDEX_NAME`
	default:
		return nil, fmt.Errorf("mysql: unknown object kind %d", kind)
	}
	rows, err := s.db.QueryContext(ctx, q, s.cfg.DBName)
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

// Columns 列出一个表的列（information_schema）。
func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	if s.cfg.DBName == "" {
		return nil, errors.New("mysql: 未选择数据库")
	}
	const q = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
	rows, err := s.db.QueryContext(ctx, q, s.cfg.DBName, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Column
	for rows.Next() {
		var (
			name, typ, nullable, key string
		)
		if err := rows.Scan(&name, &typ, &nullable, &key); err != nil {
			return nil, err
		}
		out = append(out, engine.Column{
			Name:       name,
			Type:       typ,
			Nullable:   nullable == "YES",
			PrimaryKey: key == "PRI",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Stream 逐行流式读结果（全量导出用）：不设行数上限，也不裁单元格，内存只留当前行。
func (s *session) Stream(ctx context.Context, query string, onColumns func([]string) error, onRow func([]any) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("mysql: session not open")
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
			switch t := v.(type) {
			case []byte:
				vals[i] = append([]byte(nil), t...)
			}
		}
		if err := onRow(vals); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ObjectDDL 返回表/视图的建对象语句（SHOW CREATE）；索引不支持。
func (s *session) ObjectDDL(ctx context.Context, kind engine.ObjectKind, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", errors.New("mysql: session not open")
	}
	quoted := "`" + strings.ReplaceAll(name, "`", "``") + "`"
	var q string
	switch kind {
	case engine.ObjectTable:
		q = "SHOW CREATE TABLE " + quoted
	case engine.ObjectView:
		q = "SHOW CREATE VIEW " + quoted
	default:
		return "", errors.New("mysql: 该对象不支持查看 DDL")
	}
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
	// SHOW CREATE 的最后一列是 DDL 文本。
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

// txWrap 把 database/sql 事务包成 engine.Tx。
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

// Begin 开一个显式事务（写操作提交用）。
func (s *session) Begin(ctx context.Context) (engine.Tx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &txWrap{tx: tx}, nil
}
