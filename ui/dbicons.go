package ui

import (
	"github.com/egoist/mygo/ui"
)

// 数据源品牌图标：取自 db-k 的矢量路径（24×24、实心单路径、NonZero 填充），
// 在 Go 侧重新包成 SVG 由 mygo 渲染；颜色取各产品品牌色。
const (
	svgPostgresPath = "M 7.20 8.60 C 7.20 6.23 9.26 4.30 11.80 4.30 C 14.34 4.30 16.40 6.23 16.40 8.60 " +
		"C 16.40 10.97 14.34 12.90 11.80 12.90 C 9.26 12.90 7.20 10.97 7.20 8.60 Z " +
		"M 12.60 9.80 C 12.60 7.48 14.12 5.60 16.00 5.60 C 17.88 5.60 19.40 7.48 19.40 9.80 " +
		"C 19.40 12.12 17.88 14.00 16.00 14.00 C 14.12 14.00 12.60 12.12 12.60 9.80 Z " +
		"M 6.40 10.60 L 10.80 10.40 L 8.60 16.60 L 5.20 16.40 Z " +
		"M 5.15 16.80 C 5.15 15.83 5.93 15.05 6.90 15.05 C 7.87 15.05 8.65 15.83 8.65 16.80 " +
		"C 8.65 17.77 7.87 18.55 6.90 18.55 C 5.93 18.55 5.15 17.77 5.15 16.80 Z"

	svgMySQLPath = "M 21.00 10.00 C 19.80 8.40 18.50 7.40 17.10 7.20 C 15.20 6.40 13.10 6.20 11.20 6.70 " +
		"C 9.10 7.30 7.20 8.60 5.60 10.20 " +
		"C 4.40 9.00 3.20 7.40 2.00 6.00 C 1.60 8.20 2.60 9.80 4.80 10.40 " +
		"C 3.00 11.80 2.20 13.40 2.20 14.80 C 4.00 14.20 5.20 13.00 5.80 11.60 " +
		"C 8.40 12.20 11.60 11.90 14.20 11.20 C 16.60 10.60 19.00 10.40 21.00 10.00 Z " +
		"M 13.00 7.20 C 12.90 5.60 12.80 4.40 12.40 3.40 C 11.20 4.20 10.30 5.80 9.90 7.60 Z"

	svgSQLitePath = "M 19.80 4.20 C 17.20 5.20 13.40 8.20 10.40 12.00 C 8.80 13.80 7.40 15.80 6.60 17.80 " +
		"C 8.60 17.60 11.20 15.60 13.60 12.60 C 16.00 9.60 18.60 6.60 19.80 4.20 Z " +
		"M 8.20 13.00 L 10.60 14.40 L 5.80 20.40 L 3.60 19.00 Z"

	svgClickHousePath = "M 4.00 8.90 L 4.00 15.10 C 4.00 15.87 4.63 16.50 5.40 16.50 C 6.17 16.50 6.80 15.87 6.80 15.10 " +
		"L 6.80 8.90 C 6.80 8.13 6.17 7.50 5.40 7.50 C 4.63 7.50 4.00 8.13 4.00 8.90 Z " +
		"M 8.40 4.90 L 8.40 19.10 C 8.40 19.87 9.03 20.50 9.80 20.50 C 10.57 20.50 11.20 19.87 11.20 19.10 " +
		"L 11.20 4.90 C 11.20 4.13 10.57 3.50 9.80 3.50 C 9.03 3.50 8.40 4.13 8.40 4.90 Z " +
		"M 12.80 6.90 L 12.80 17.10 C 12.80 17.87 13.43 18.50 14.20 18.50 C 14.97 18.50 15.60 17.87 15.60 17.10 " +
		"L 15.60 6.90 C 15.60 6.13 14.97 5.50 14.20 5.50 C 13.43 5.50 12.80 6.13 12.80 6.90 Z " +
		"M 17.20 9.65 L 17.20 14.35 C 17.20 15.12 17.83 15.75 18.60 15.75 C 19.37 15.75 20.00 15.12 20.00 14.35 " +
		"L 20.00 9.65 C 20.00 8.88 19.37 8.25 18.60 8.25 C 17.83 8.25 17.20 8.88 17.20 9.65 Z"
)

func brandSVG(name, path string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="` + path + `"/></svg>`))
}

var (
	svgPostgres   = brandSVG("postgres", svgPostgresPath)
	svgMySQL      = brandSVG("mysql", svgMySQLPath)
	svgSQLite     = brandSVG("sqlite", svgSQLitePath)
	svgClickHouse = brandSVG("clickhouse", svgClickHousePath)
)

// connKindIcon 返回数据源类型图标；未知类型给 nil。
func connKindIcon(kind string) *ui.SVG {
	switch kind {
	case "sqlite":
		return svgSQLite
	case "mysql":
		return svgMySQL
	case "postgres":
		return svgPostgres
	case "clickhouse":
		return svgClickHouse
	}
	return nil
}

// connKindColor 返回数据源品牌色（取自 db-k 的 badgeColor）。
func connKindColor(kind string) ui.Color {
	switch kind {
	case "sqlite":
		return ui.RGB(0x0F, 0x80, 0xCC)
	case "mysql":
		return ui.RGB(0x00, 0x75, 0x8F)
	case "postgres":
		return ui.RGB(0x33, 0x67, 0x91)
	case "clickhouse":
		return ui.RGB(0xB8, 0x86, 0x0B) // 官方黄对比度太低，取暗金
	}
	return ui.RGB(0x80, 0x80, 0x80)
}
