package sqlite

import (
	"testing"

	"db-g/engine"
)

func TestScopedCatalogInterfaces(t *testing.T) {
	s := engine.Session(&session{})
	checks := []struct {
		name string
		ok   bool
	}{
		{"ObjectsIn", implements[engine.SchemaObjectLister](s)},
		{"ColumnsIn", implements[engine.SchemaColumnLister](s)},
		{"IndexesIn", implements[engine.SchemaIndexLister](s)},
		{"ObjectDDLIn", implements[engine.SchemaObjectDDLer](s)},
		{"ObjectStatsIn", implements[engine.SchemaObjectStatter](s)},
	}
	for _, c := range checks {
		if !c.ok {
			t.Fatalf("%s missing", c.name)
		}
	}
}

func implements[T any](v any) bool { _, ok := v.(T); return ok }
