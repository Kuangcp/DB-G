package engine

// Capabilities 是数据源能力位；UI 按位降级功能。
type Capabilities uint32

const (
	CapEditableResult Capabilities = 1 << iota
	CapFetchMore
	CapObjectDDL
	CapSQLCompletion
	CapObjects
	CapColumns
	CapExplain
)

func (c Capabilities) Has(x Capabilities) bool { return c&x != 0 }
