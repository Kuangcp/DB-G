package engine

import (
	"context"
	"testing"
)

type stubSession struct{}

func (stubSession) Open(context.Context) error { return nil }
func (stubSession) Close() error               { return nil }
func (stubSession) RunStatement(context.Context, string) (*QueryResult, error) {
	return nil, nil
}
func (stubSession) Cancel() bool       { return false }
func (stubSession) Caps() Capabilities { return 0 }

func TestRegisterOpen(t *testing.T) {
	if _, err := Open(Profile{Kind: "definitely-unknown"}); err == nil {
		t.Fatal("unknown kind must return an error")
	}
	Register("stubkind", func(p Profile) (Session, error) { return stubSession{}, nil })
	s, err := Open(Profile{Kind: "stubkind", Name: "n1", DSN: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(stubSession); !ok {
		t.Fatalf("got %T, want stubSession", s)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register must panic")
		}
	}()
	Register("stubkind", func(p Profile) (Session, error) { return stubSession{}, nil })
}

func TestCapabilitiesHas(t *testing.T) {
	var c Capabilities = CapEditableResult | CapFetchMore
	if !c.Has(CapEditableResult) || !c.Has(CapFetchMore) {
		t.Fatal("set bits must report true")
	}
	if c.Has(CapObjectDDL) {
		t.Fatal("unset bit must report false")
	}
}

func TestCapObjects(t *testing.T) {
	var c Capabilities = CapObjects
	if !c.Has(CapObjects) {
		t.Fatal("CapObjects must be settable and readable")
	}
	if CapObjects == CapSQLCompletion || CapObjects == CapEditableResult ||
		CapObjects == CapFetchMore || CapObjects == CapObjectDDL {
		t.Fatal("CapObjects must not collide with existing bits")
	}
	if CapObjects != 1<<4 {
		t.Fatalf("CapObjects = %d, want %d", CapObjects, 1<<4)
	}
}

func TestCapColumns(t *testing.T) {
	var c Capabilities = CapColumns
	if !c.Has(CapColumns) {
		t.Fatal("CapColumns must be settable and readable")
	}
	for _, other := range []Capabilities{CapEditableResult, CapFetchMore, CapObjectDDL, CapSQLCompletion, CapObjects} {
		if CapColumns == other {
			t.Fatal("CapColumns must not collide with existing bits")
		}
	}
}
