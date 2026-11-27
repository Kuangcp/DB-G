package engine

import "sort"

// DriverMeta 是数据源的展示/方言元数据。驱动在 Register 时提供，
// UI 不再按 kind 散落 switch；图标/品牌色等纯视觉信息仍留在 ui。
type DriverMeta struct {
	Kind            string
	Order           int            // 展示顺序，小的在前
	Label           string         // 数据源名称（品牌名，不翻译）
	DefaultPort     int            // 0 = 无端口概念（SQLite）
	DefaultProtocol string         // ClickHouse: native/http/https；其他为空
	Protocols       []string       // 可选协议；空 = 固定
	ProtocolPorts   map[string]int // 各协议默认端口（可选）
	CountExpr       string         // 统计总行数用的表达式；空 = COUNT(*)
	QuoteIdent      func(string) string
}

// Normalized 返回填好默认值的元数据副本。
func (m DriverMeta) Normalized() DriverMeta {
	if m.Label == "" {
		m.Label = m.Kind
	}
	if m.CountExpr == "" {
		m.CountExpr = "COUNT(*)"
	}
	if m.QuoteIdent == nil {
		m.QuoteIdent = func(s string) string { return `"` + s + `"` }
	}
	return m
}

var driverOrder []string

// Meta 返回某种数据源的元数据。
func Meta(kind string) (DriverMeta, bool) {
	regMu.RLock()
	r, ok := registry[kind]
	regMu.RUnlock()
	if !ok {
		return DriverMeta{}, false
	}
	return r.meta, true
}

// DriverMetas 返回全部已注册数据源的元数据，按 Order、Label 排序。
func DriverMetas() []DriverMeta {
	regMu.RLock()
	out := make([]DriverMeta, 0, len(driverOrder))
	for _, kind := range driverOrder {
		if r, ok := registry[kind]; ok {
			out = append(out, r.meta)
		}
	}
	regMu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Label < out[j].Label
	})
	return out
}
