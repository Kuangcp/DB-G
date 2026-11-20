package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

// 统计弹窗固定尺寸：维度数量随数据源变化，面板不跟着一弹一弹。
const (
	statsPanelWidth  = 680
	statsPanelHeight = 560
)

// showStats 取表的统计信息并弹窗（headless 同步，有窗口时后台 + Update）。
func (a *root) showStats(p app.ConnProfile, o engine.DBObject) {
	// 统计只对表开放：视图要展开成结果集才有行数，代价不可控。
	if o.Kind != engine.ObjectTable {
		return
	}
	if !a.connected(p.ID) {
		if err := a.openConn(p); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	sess := a.sessionFor(p.ID)
	a.statsOpen, a.statsLoading = true, true
	a.statsTitle, a.statsKind = o.Name, p.Kind
	a.stats, a.statsErr, a.statsSQLOpen = nil, "", false

	load := func() {
		statter, hasStats := sess.(engine.ObjectStatter)
		scoped, hasScoped := sess.(engine.SchemaObjectStatter)
		if sess == nil || !sess.Caps().Has(engine.CapObjectStats) || (!hasStats && !hasScoped) {
			unsupported := func() { a.setStatsNote(locales.StatsUnsupported()) }
			if a.win != nil {
				a.safeUpdate(a.win, unsupported)
				return
			}
			unsupported()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var (
			stats *engine.ObjectStats
			err   error
		)
		if hasScoped {
			stats, err = scoped.ObjectStatsIn(ctx, o.Schema, o.Kind, o.Name)
		} else {
			stats, err = statter.ObjectStats(ctx, o.Kind, o.Name)
		}
		apply := func() {
			if err != nil {
				a.setStatsNote(locales.StatsFailed(err.Error()))
				app.LogWarn("catalog", "stats load failed", "object", o.Name, "err", err.Error())
				return
			}
			a.setStats(stats)
		}
		if a.win != nil {
			a.safeUpdate(a.win, apply)
			return
		}
		apply()
	}
	if a.win == nil {
		load()
		return
	}
	go load()
}

// setStats 把统计结果装进弹窗；SQL 默认折叠，但缓冲区先备好，复制不受折叠影响。
func (a *root) setStats(stats *engine.ObjectStats) {
	a.stats, a.statsErr, a.statsLoading = stats, "", false
	view := a.statsSQLView()
	view.Buffer.SetLanguage(sqlLanguageForKind(a.statsKind))
	view.Buffer.Reset(joinStatsSQL(stats))
}

// setStatsNote 在弹窗里显示一行提示（不支持 / 读取失败）。
func (a *root) setStatsNote(msg string) {
	a.stats, a.statsErr, a.statsLoading = nil, msg, false
}

// statsSQLView 返回统计 SQL 的只读高亮视图（首次用时创建）。
func (a *root) statsSQLView() *myeditor.State {
	if a.statsSQLState == nil {
		a.statsSQLState = &myeditor.State{
			Buffer:       myeditor.NewBuffer(""),
			ReadOnly:     true,
			LineNumbers:  myeditor.LineNumbersOff,
			NoActiveLine: true,
			NoFolding:    true,
		}
	}
	return a.statsSQLState
}

// joinStatsSQL 把驱动实际执行的语句拼成可复制的脚本文本。
func joinStatsSQL(stats *engine.ObjectStats) string {
	if stats == nil {
		return ""
	}
	parts := make([]string, 0, len(stats.SQL))
	for _, q := range stats.SQL {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if !strings.HasSuffix(q, ";") {
			q += ";"
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, "\n\n")
}

// statsPanel 渲染统计弹窗：常用指标 + 数据源特有项 + 可折叠、可复制的统计 SQL。
func (a *root) statsPanel(c *ui.Context) {
	if !a.statsOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &a.statsOpen, func() {
		ui.Text(c, locales.StatsTitle(a.statsTitle)).Bold().SingleLine()
		switch {
		case a.statsLoading:
			ui.Text(c, locales.StatsLoading()).TextColor(t.TextMuted)
		case a.statsErr != "":
			note := ui.Text(c, a.statsErr).SingleLine().TextColor(t.Danger)
			note.Tooltip(a.statsErr)
		case a.stats != nil:
			bodyHeight := float32(420)
			if a.statsSQLOpen {
				bodyHeight = 270
			}
			ui.Scroll(c).Height(bodyHeight).Children(func() {
				a.statsBody(c)
			})
			header := ui.ButtonBase(c).Label(locales.StatsSQL()).Gap(6).AlignItems(ui.Center).
				Padding(t.Space(0.75), t.Space(1.5)).Radius(t.Space(1)).FillWidth()
			if header.Hovered() {
				header.Background(t.SurfaceHover)
			}
			header.Children(func() {
				ic := ui.Icon(c, iconChevronDown).FontSize(14)
				if !a.statsSQLOpen {
					ic.Rotate(-90)
				}
				ic.TextColor(t.TextMuted)
				ui.Text(c, locales.StatsSQL())
				ui.Spacer(c)
				if n := len(a.stats.SQL); n == 0 {
					ui.Text(c, locales.StatsSQLEmpty()).FontSize(11).TextColor(t.TextMuted)
				} else {
					ui.Text(c, locales.StatsSQLCount(n)).FontSize(11).TextColor(t.TextMuted)
				}
			})
			if header.Clicked() {
				a.statsSQLOpen = !a.statsSQLOpen
			}
			if a.statsSQLOpen {
				myeditor.View(c, a.statsSQLView()).FillWidth().Height(150)
			}
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if a.stats != nil && len(a.stats.SQL) > 0 &&
				ui.Button(c, locales.HistoryCopySQL()).Tooltip(locales.StatsCopySQLTip()).Clicked() {
				c.WriteClipboard(joinStatsSQL(a.stats))
			}
			if ui.PrimaryButton(c, locales.CommonClose()).Clicked() {
				a.statsOpen = false
			}
		})
	}).Width(statsPanelWidth).Height(statsPanelHeight)
}

// statsBody 渲染通用指标与数据源特有统计。
func (a *root) statsBody(c *ui.Context) {
	count := func(v *int64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatInt(*v, 10)
	}
	size := func(v *int64) (string, string) {
		if v == nil {
			return "—", ""
		}
		return formatByteSize(*v), strconv.FormatInt(*v, 10) + " B"
	}
	statsKV(c, locales.StatsRows(), count(a.stats.Rows), "")
	statsKV(c, locales.StatsColumns(), count(a.stats.Columns), "")
	value, tip := size(a.stats.DataBytes)
	statsKV(c, locales.StatsDataSize(), value, tip)
	value, tip = size(a.stats.IndexBytes)
	statsKV(c, locales.StatsIndexSize(), value, tip)
	for _, item := range a.stats.Extra {
		value, tip = statDisplay(item.Key, item.Value)
		statsKV(c, statLabel(item.Key), value, tip)
	}
}

// statsKV 是统计面板里的一行：左侧固定宽度的弱化标签 + 右侧值。
func statsKV(c *ui.Context, label, value, tip string) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(t.Space(0.5), t.Space(0.5)).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(150).SingleLine()
		v := ui.Text(c, value).SingleLine()
		if tip != "" && tip != value {
			v.Tooltip(tip)
		}
		ui.Spacer(c)
	})
}

// isByteStat 报告该统计值是否按字节数展示。
func isByteStat(key string) bool {
	switch key {
	case "statSQLitePageSize", "statPostgresTotalBytes", "statClickHouseTotalBytes",
		"statClickHouseUncompressedBytes":
		return true
	}
	return false
}

// statDisplay 把已知统计值格式化成人能读的形式（字节数），未知键原样显示。
func statDisplay(key, value string) (string, string) {
	if isByteStat(key) {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return formatByteSize(n), value + " B"
		}
	}
	return value, ""
}

// formatByteSize 把字节数格式化成 B/KiB/MiB/GiB/TiB。
func formatByteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// statLabel 返回数据源特有统计项的本地化标签；未知键原样显示。
func statLabel(key string) string {
	switch key {
	case "statSQLitePageSize":
		return locales.StatSQLitePageSize()
	case "statSQLitePageCount":
		return locales.StatSQLitePageCount()
	case "statSQLiteFreelist":
		return locales.StatSQLiteFreelist()
	case "statSQLiteRootPage":
		return locales.StatSQLiteRootPage()
	case "statMySQLEstimatedRows":
		return locales.StatMySQLEstimatedRows()
	case "statMySQLEngine":
		return locales.StatMySQLEngine()
	case "statMySQLCollation":
		return locales.StatMySQLCollation()
	case "statMySQLAutoIncrement":
		return locales.StatMySQLAutoIncrement()
	case "statMySQLRowFormat":
		return locales.StatMySQLRowFormat()
	case "statMySQLComment":
		return locales.StatMySQLComment()
	case "statMySQLCreateTime":
		return locales.StatMySQLCreateTime()
	case "statMySQLUpdateTime":
		return locales.StatMySQLUpdateTime()
	case "statPostgresTotalBytes":
		return locales.StatPostgresTotalBytes()
	case "statPostgresLiveTuples":
		return locales.StatPostgresLiveTuples()
	case "statPostgresDeadTuples":
		return locales.StatPostgresDeadTuples()
	case "statPostgresLastVacuum":
		return locales.StatPostgresLastVacuum()
	case "statPostgresLastAutovacuum":
		return locales.StatPostgresLastAutovacuum()
	case "statPostgresLastAnalyze":
		return locales.StatPostgresLastAnalyze()
	case "statPostgresLastAutoanalyze":
		return locales.StatPostgresLastAutoanalyze()
	case "statClickHouseEngine":
		return locales.StatClickHouseEngine()
	case "statClickHouseSortingKey":
		return locales.StatClickHouseSortingKey()
	case "statClickHousePrimaryKey":
		return locales.StatClickHousePrimaryKey()
	case "statClickHouseTotalRows":
		return locales.StatClickHouseTotalRows()
	case "statClickHouseTotalBytes":
		return locales.StatClickHouseTotalBytes()
	case "statClickHouseParts":
		return locales.StatClickHouseParts()
	case "statClickHouseUncompressedBytes":
		return locales.StatClickHouseUncompressedBytes()
	case "statClickHousePrimaryKeyMemory":
		return locales.StatClickHousePrimaryKeyMemory()
	case "statClickHouseCompressionRatio":
		return locales.StatClickHouseCompressionRatio()
	case "statError":
		return locales.StatError()
	}
	return key
}
