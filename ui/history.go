package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// historyPanel 渲染 SQL 历史面板：当前数据源最近 100 条，可插入光标或复制。
// 完整记录在面板标题显示的 <connectionID>.jsonl 里。
func (a *root) historyPanel(c *ui.Context) {
	if !a.modal.HistoryOpen {
		return
	}
	connID := a.activeConnID()
	entries := []app.HistoryEntry(nil)
	path := ""
	if a.history != nil && connID != "" {
		entries = a.history.List(connID, 100)
		path = a.history.Path(connID)
	}
	ui.Modal(c, &a.modal.HistoryOpen, func() {
		title := locales.HistoryTitle()
		if connID != "" {
			title = locales.HistoryTitleConn(a.activeConnName(), len(entries))
		}
		ui.Text(c, title).Bold().SingleLine()
		if connID == "" {
			ui.Text(c, locales.HistoryNoDatasource()).TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, path).FontSize(11).TextColor(c.Theme().TextMuted).
				SingleLine().Tooltip(locales.HistoryFileTooltip())
			ui.Text(c, locales.HistoryPanelHint()).
				FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		}
		if len(entries) == 0 && connID != "" {
			ui.Text(c, locales.HistoryEmpty()).TextColor(c.Theme().TextMuted)
		}
		ui.Scroll(c).Height(360).Children(func() {
			for _, e := range entries {
				a.historyRow(c, e)
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.PrimaryButton(c, locales.CommonClose()).Clicked() {
				a.modal.HistoryOpen = false
			}
		})
	})
}

func (a *root) historyRow(c *ui.Context, e app.HistoryEntry) {
	status := "✓"
	if !e.OK {
		status = "✗"
	}
	when := time.UnixMilli(e.Time).Format("01-02 15:04:05")
	summary := firstLine(e.SQL)
	if r := []rune(summary); len(r) > 80 {
		summary = string(r[:80]) + "…"
	}
	ui.Row(c).Gap(8).PaddingEnd(8).Children(func() {
		ui.Text(c, status).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, when).FontSize(12).TextColor(c.Theme().TextMuted)
		if e.ConnName != "" {
			ui.Text(c, e.ConnName).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		}
		ui.Text(c, fmt.Sprintf("%dms", e.DurMS)).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, summary).FontSize(12).SingleLine().Grow(1)
		if iconButtonPlain(c, iconInsert, locales.HistoryInsertAtCursor(), ui.Transparent).Clicked() {
			a.code.Insert(e.SQL)
			a.modal.HistoryOpen = false
		}
		if iconButtonPlain(c, iconCopy, locales.HistoryCopySQL(), ui.Transparent).Clicked() {
			c.WriteClipboard(e.SQL)
		}
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
