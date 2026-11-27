package mysql

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
}
