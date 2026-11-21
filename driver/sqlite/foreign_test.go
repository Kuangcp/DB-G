package sqlite

import (
	"context"
	"testing"

	"db-g/engine"
)

func TestForeignKeys(t *testing.T) {
	ctx := context.Background()
	s := newMemSession(t)
	for _, q := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))`,
	} {
		if _, err := s.RunStatement(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	fk, ok := s.(engine.ForeignKeyer)
	if !ok || !s.Caps().Has(engine.CapForeignKeys) {
		t.Fatal("sqlite must provide foreign keys")
	}
	refs, err := fk.ForeignKeys(ctx, "", "orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Column != "user_id" || refs[0].RefTable != "users" || refs[0].RefColumn != "id" {
		t.Fatalf("refs: %+v", refs)
	}
}
