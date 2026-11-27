package sqlite

import (
	"testing"

	"db-g/engine"
)

func TestScopedCatalogInterfacesOnly(t *testing.T) {
	s := engine.Session(&session{})
	if _, ok := s.(engine.ObjectLister); ok {
		t.Fatal("flat ObjectLister must be removed")
	}
	if _, ok := s.(engine.ColumnLister); ok {
		t.Fatal("flat ColumnLister must be removed")
	}
	if _, ok := s.(engine.IndexLister); ok {
		t.Fatal("flat IndexLister must be removed")
	}
	if _, ok := s.(engine.ObjectDDLer); ok {
		t.Fatal("flat ObjectDDLer must be removed")
	}
	if _, ok := s.(engine.ObjectStatter); ok {
		t.Fatal("flat ObjectStatter must be removed")
	}
	if _, ok := s.(engine.SchemaObjectLister); !ok {
		t.Fatal("ObjectsIn missing")
	}
	if _, ok := s.(engine.SchemaColumnLister); !ok {
		t.Fatal("ColumnsIn missing")
	}
	if _, ok := s.(engine.SchemaIndexLister); !ok {
		t.Fatal("IndexesIn missing")
	}
	if _, ok := s.(engine.SchemaObjectDDLer); !ok {
		t.Fatal("ObjectDDLIn missing")
	}
	if _, ok := s.(engine.SchemaObjectStatter); !ok {
		t.Fatal("ObjectStatsIn missing")
	}
}
