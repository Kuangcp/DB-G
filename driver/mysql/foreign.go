package mysql

import (
	"context"

	"db-g/engine"
)

// ForeignKeys 从 information_schema 取表的外键（只服务 AI 检索扩展）。
func (s *session) ForeignKeys(ctx context.Context, schema, table string) ([]engine.ForeignKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, engine.NewUserError(engine.ErrSessionClosed, "")
	}
	schema, err := s.schemaLocked(ctx, schema)
	if err != nil {
		return nil, err
	}
	const q = `SELECT COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY ORDINAL_POSITION`
	rows, err := s.db.QueryContext(ctx, q, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.ForeignKey
	for rows.Next() {
		var ref engine.ForeignKey
		if err := rows.Scan(&ref.Column, &ref.RefSchema, &ref.RefTable, &ref.RefColumn); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
