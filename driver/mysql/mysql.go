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
	rowLimit     = 500
	maxCellBytes = 1 << 20
)

func init() {
	engine.Register("mysql", openSession)
}

type session struct {
	cfg       *mysql.Config
	db        *sql.DB
	mu        sync.Mutex
	curCancel atomic.Pointer[context.CancelFunc]
	schema    string // 期望的当前库（UseSchema 设置）
	applied   string // 已应用到连接池的库
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
	// 额外连接参数排在默认值之后，用户想改 charset/parseTime 之类能覆盖掉。
	params, err := engine.Params(p.Params)
	if err != nil {
		return nil, fmt.Errorf("mysql: %w", err)
	}
	for k, v := range params {
		cfg.Params[k] = v
	}
	return &session{cfg: cfg}, nil
}

func (s *session) Open(ctx context.Context) error {
	db, err := sql.Open("mysql", s.cfg.FormatDSN())
	if err != nil {
		return err
	}
	// USE 只作用于一条连接；限制为 1 条，切换库后连接池里的连接（也只有它）保持一致。
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

func (s *session) Caps() engine.Capabilities {
	return engine.CapObjects | engine.CapColumns | engine.CapFetchMore | engine.CapObjectDDL | engine.CapEditableResult | engine.CapExplain | engine.CapExplainAnalyze
}

// ServerInfo 返回当前库名与 MySQL 版本；未选库时 DATABASE() 为 NULL。
func (s *session) ServerInfo(ctx context.Context) (engine.ServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return engine.ServerInfo{}, errors.New("mysql: session not open")
	}
	var version string
	var database sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT VERSION(), DATABASE()").Scan(&version, &database); err != nil {
		return engine.ServerInfo{}, err
	}
	return engine.ServerInfo{Database: database.String, Version: version}, nil
}

func (s *session) Cancel() bool {
	c := s.curCancel.Load()
	if c == nil {
		return false
	}
	(*c)()
	return true
}

// Explain 返回 MySQL 的 JSON 计划；ANALYZE 走 EXPLAIN ANALYZE（TREE 输出）。
func (s *session) Explain(ctx context.Context, query string, opts engine.ExplainOptions) (*engine.QueryResult, error) {
	if opts.Analyze {
		return s.RunStatement(ctx, "EXPLAIN ANALYZE "+query)
	}
	return s.RunStatement(ctx, "EXPLAIN FORMAT=JSON "+query)
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
	if err := s.applySchemaLocked(ctx); err != nil {
		return nil, err
	}
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
	if cts, err := rows.ColumnTypes(); err == nil && len(cts) == len(cols) {
		out.ColumnTypes = make([]string, len(cols))
		for i, ct := range cts {
			out.ColumnTypes[i] = ct.DatabaseTypeName()
		}
	}
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

// quoteIdent 是 MySQL 的标识符引用（库名/表名）。
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
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

// applySchemaLocked 把期望的库应用到连接：连接池只有一条连接，USE 之后会一直保留。
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

// UseSchema 切换当前库（MySQL 的 USE）；后续语句与元数据查询都以它为准。
func (s *session) UseSchema(ctx context.Context, schema string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("mysql: session not open")
	}
	s.schema = schema
	return s.applySchemaLocked(ctx)
}

// ActiveSchema 返回当前库；没选库时返回空串（不报错，调用方可以自己选一个）。
func (s *session) ActiveSchema(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", errors.New("mysql: session not open")
	}
	if s.schema != "" {
		return s.schema, nil
	}
	var name sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return "", err
	}
	if !name.Valid {
		return "", nil
	}
	return name.String, nil
}

// schemaLocked 解析查询要用的库：显式传入优先，其次当前库/连接配置，都没有才报错。
func (s *session) schemaLocked(ctx context.Context, schema string) (string, error) {
	if schema != "" {
		return schema, nil
	}
	if s.schema != "" {
		return s.schema, nil
	}
	if s.cfg.DBName == "" {
		return "", errors.New("mysql: 未选择数据库")
	}
	var name sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return "", err
	}
	if !name.Valid || name.String == "" {
		return "", errors.New("mysql: 未选择数据库")
	}
	return name.String, nil
}

func mysqlObjectQuery(kind engine.ObjectKind) (string, error) {
	switch kind {
	case engine.ObjectTable:
		return `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'`, nil
	case engine.ObjectView:
		return `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'VIEW'`, nil
	case engine.ObjectIndex:
		return `SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? GROUP BY INDEX_NAME`, nil
	default:
		return "", fmt.Errorf("mysql: unknown object kind %d", kind)
	}
}

// ObjectCounts 只查 information_schema 的计数，供大库树分组标题使用。
func (s *session) ObjectCounts(ctx context.Context, schema string) (map[engine.ObjectKind]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	out := map[engine.ObjectKind]int{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT TABLE_TYPE, COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? GROUP BY TABLE_TYPE`, schema)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			rows.Close()
			return nil, err
		}
		switch typ {
		case "BASE TABLE":
			out[engine.ObjectTable] = n
		case "VIEW":
			out[engine.ObjectView] = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var indexes int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ?`, schema).Scan(&indexes); err != nil {
		return nil, err
	}
	out[engine.ObjectIndex] = indexes
	return out, nil
}

// ObjectsPage 按页列出某库的对象；limit <= 0 表示不分页。
func (s *session) ObjectsPage(ctx context.Context, schema string, kind engine.ObjectKind, offset, limit int) ([]engine.DBObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	q, err := mysqlObjectQuery(kind)
	if err != nil {
		return nil, err
	}
	q += ` ORDER BY 1`
	args := []any{schema}
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
		out = append(out, engine.DBObject{Kind: kind, Name: name, Schema: schema})
	}
	return out, rows.Err()
}

// ObjectsIn 列出某库的表/视图/索引。
func (s *session) ObjectsIn(ctx context.Context, schema string, kind engine.ObjectKind) ([]engine.DBObject, error) {
	return s.ObjectsPage(ctx, schema, kind, 0, 0)
}

func (s *session) Objects(ctx context.Context, kind engine.ObjectKind) ([]engine.DBObject, error) {
	return s.ObjectsPage(ctx, "", kind, 0, 0)
}

// Columns 列出一个表的列；没选库时用当前库，当前库也没有就报错。
func (s *session) Columns(ctx context.Context, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.columnsLocked(ctx, "", table)
}

// ColumnsIn 按库列出列：补全和多库对象树走这里，连接没填数据库也能用。
func (s *session) ColumnsIn(ctx context.Context, schema, table string) ([]engine.Column, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.columnsLocked(ctx, schema, table)
}

func (s *session) columnsLocked(ctx context.Context, schema, table string) ([]engine.Column, error) {
	if s.db == nil {
		return nil, errors.New("mysql: session not open")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	const q = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
	rows, err := s.db.QueryContext(ctx, q, schema, table)
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
	return s.objectDDLLocked(ctx, "", kind, name)
}

// ObjectDDLIn 按库取 DDL：对象树跨库时不会被当前库带偏。
func (s *session) ObjectDDLIn(ctx context.Context, schema string, kind engine.ObjectKind, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objectDDLLocked(ctx, schema, kind, name)
}

func (s *session) objectDDLLocked(ctx context.Context, schema string, kind engine.ObjectKind, name string) (string, error) {
	if s.db == nil {
		return "", errors.New("mysql: session not open")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return "", err
	}
	quoted := quoteIdent(schema) + "." + quoteIdent(name)
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
	if err := s.applySchemaLocked(ctx); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &txWrap{tx: tx}, nil
}
