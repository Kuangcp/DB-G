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
	CapExplainAnalyze
	CapObjectStats
	CapForeignKeys
	CapIndexes
)

type capabilityInfo struct {
	bit  Capabilities
	name string
}

// capabilityInfos 是能力位的稳定名称；新增位必须同时在这里登记（capabilities_test 会守住）。
var capabilityInfos = []capabilityInfo{
	{CapEditableResult, "editable-result"},
	{CapFetchMore, "fetch-more"},
	{CapObjectDDL, "object-ddl"},
	{CapSQLCompletion, "sql-completion"},
	{CapObjects, "objects"},
	{CapColumns, "columns"},
	{CapExplain, "explain"},
	{CapExplainAnalyze, "explain-analyze"},
	{CapObjectStats, "object-stats"},
	{CapForeignKeys, "foreign-keys"},
	{CapIndexes, "indexes"},
}

func (c Capabilities) Has(x Capabilities) bool { return c&x != 0 }
