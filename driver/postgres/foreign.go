package postgres

import (
	"context"

	"db-g/engine"
)

// ForeignKeys 从 pg_constraint 取表的外键，按列顺序展开复合外键。
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
	if schema == "" {
		return nil, engine.NewUserError(engine.ErrNoSchemaSelected, "")
	}
	const q = `
SELECT a.attname, fn.nspname, fc.relname, fa.attname
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_class fc ON fc.oid = con.confrelid
JOIN pg_namespace fn ON fn.oid = fc.relnamespace
CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(attnum, fattnum, ord)
JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
JOIN pg_attribute fa ON fa.attrelid = con.confrelid AND fa.attnum = k.fattnum
WHERE con.contype = 'f' AND n.nspname = $1 AND c.relname = $2
ORDER BY k.ord`
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
