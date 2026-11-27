package sqlite

import (
	"context"
	"database/sql"

	"db-g/engine"
)

// ForeignKeys 用 PRAGMA foreign_key_list 取表的外键（只服务 AI 检索扩展）。
func (s *session) ForeignKeys(ctx context.Context, _ string, table string) ([]engine.ForeignKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, engine.NewUserError(engine.ErrSessionClosed, "")
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.ForeignKey
	for rows.Next() {
		var (
			id, seq                   int
			refTable, from            string
			to                        sql.NullString
			onUpdate, onDelete, match sql.NullString
		)
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		out = append(out, engine.ForeignKey{Column: from, RefTable: refTable, RefColumn: to.String})
	}
	return out, rows.Err()
}
