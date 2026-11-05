package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// historyPanel 渲染 SQL 历史面板：最近 200 条，可插入光标或复制。
func (a *root) historyPanel(c *ui.Context) {
	if !a.historyOpen {
		return
	}
	entries := []app.HistoryEntry(nil)
	path := ""
	if a.history != nil {
		entries = a.history.List(200)
		path = a.history.Path()
	}
	ui.Modal(c, &a.historyOpen, func() {
		ui.Text(c, fmt.Sprintf("SQL 历史（%d 条 · %s）", len(entries), path)).Bold().SingleLine()
		if len(entries) == 0 {
			ui.Text(c, "还没有执行记录").TextColor(c.Theme().TextMuted)
		}
		ui.Scroll(c).Height(360).Children(func() {
			for _, e := range entries {
				a.historyRow(c, e)
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.PrimaryButton(c, "关闭").Clicked() {
				a.historyOpen = false
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
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, status).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, when).FontSize(12).TextColor(c.Theme().TextMuted)
		if e.ConnName != "" {
			ui.Text(c, e.ConnName).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		}
		ui.Text(c, fmt.Sprintf("%dms", e.DurMS)).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, summary).FontSize(12).SingleLine().Grow(1)
		if ui.Button(c, "插入").Clicked() {
			a.code.Insert(e.SQL)
			a.historyOpen = false
		}
		if ui.Button(c, "复制").Clicked() {
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
