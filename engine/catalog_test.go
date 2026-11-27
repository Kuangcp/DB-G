package engine

import (
	"context"
	"errors"
	"testing"
)

type scopedStub struct{ stubSession }

func (scopedStub) ColumnsIn(context.Context, string, string) ([]Column, error) {
	return []Column{{Name: "scoped"}}, nil
}
func (scopedStub) ObjectsIn(context.Context, string, ObjectKind) ([]DBObject, error) {
	return []DBObject{{Name: "s", Schema: "x"}}, nil
}
func (scopedStub) IndexesIn(context.Context, string, string) ([]Index, error) {
	return []Index{{Name: "si"}}, nil
}
func (scopedStub) ObjectDDLIn(context.Context, string, ObjectKind, string) (string, error) {
	return "scoped ddl", nil
}
func (scopedStub) ObjectStatsIn(context.Context, string, ObjectKind, string) (*ObjectStats, error) {
	return &ObjectStats{}, nil
}

type flatStub struct{ stubSession }

func (flatStub) Columns(context.Context, string) ([]Column, error) {
	return []Column{{Name: "flat"}}, nil
}
func (flatStub) Objects(context.Context, ObjectKind) ([]DBObject, error) {
	return []DBObject{{Name: "f"}}, nil
}
func (flatStub) Indexes(context.Context, string) ([]Index, error) {
	return []Index{{Name: "fi"}}, nil
}
func (flatStub) ObjectDDL(context.Context, ObjectKind, string) (string, error) {
	return "flat ddl", nil
}
func (flatStub) ObjectStats(context.Context, ObjectKind, string) (*ObjectStats, error) {
	return &ObjectStats{}, nil
}

func TestCatalogResolution(t *testing.T) {
	ctx := context.Background()
	cols, err := ColumnsOf(ctx, scopedStub{}, "s", "t")
	if err != nil || cols[0].Name != "scoped" {
		t.Fatalf("scoped columns = %+v %v", cols, err)
	}
	cols, err = ColumnsOf(ctx, flatStub{}, "", "t")
	if err != nil || cols[0].Name != "flat" {
		t.Fatalf("flat columns = %+v %v", cols, err)
	}
	if _, err := ColumnsOf(ctx, stubSession{}, "", "t"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported err = %v", err)
	}
	objs, err := ObjectsOf(ctx, scopedStub{}, "x", ObjectTable)
	if err != nil || objs[0].Schema != "x" {
		t.Fatalf("objects = %+v %v", objs, err)
	}
	if ddl, err := ObjectDDLOf(ctx, scopedStub{}, "x", ObjectTable, "t"); err != nil || ddl != "scoped ddl" {
		t.Fatalf("ddl = %q %v", ddl, err)
	}
}
