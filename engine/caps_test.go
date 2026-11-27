package engine

import "testing"

func TestEveryCapabilityHasName(t *testing.T) {
	all := CapEditableResult | CapFetchMore | CapObjectDDL | CapSQLCompletion |
		CapObjects | CapColumns | CapExplain | CapExplainAnalyze |
		CapObjectStats | CapForeignKeys | CapIndexes
	seen := map[Capabilities]bool{}
	for _, info := range capabilityInfos {
		if info.name == "" {
			t.Fatalf("empty name for %d", info.bit)
		}
		if seen[info.bit] {
			t.Fatalf("duplicate name for %d", info.bit)
		}
		seen[info.bit] = true
		if all&info.bit == 0 {
			t.Fatalf("name for unknown bit %d", info.bit)
		}
	}
	for bit := Capabilities(1); bit <= CapIndexes; bit <<= 1 {
		if !seen[bit] {
			t.Fatalf("capability bit %d has no name", bit)
		}
	}
}
