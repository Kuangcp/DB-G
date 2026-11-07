package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// historyPanel 渲染 SQL 历史面板：当前数据源最近 100 条，可插入光标或复制。
// 完整记录在面板标题显示的 <connectionID>.jsonl 里。
func (a *root) historyPanel(c *ui.Context) {
	if !a.historyOpen {
		return
	}
	connID := a.activeConnID()
	entries := []app.HistoryEntry(nil)
	path := ""
	if a.history != nil && connID != "" {
		entries = a.history.List(connID, 100)
		path = a.history.Path(connID)
	}
	ui.Modal(c, &a.historyOpen, func() {
		title := "SQL 历史"
		if connID != "" {
			title = fmt.Sprintf("SQL 历史 · %s（最近 %d 条）", a.activeConnName(), len(entries))
		}
		ui.Text(c, title).Bold().SingleLine()
		if connID == "" {
			ui.Text(c, "请先连接数据源").TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, path).FontSize(11).TextColor(c.Theme().TextMuted).
				SingleLine().Tooltip("完整历史（最多保留 5000 条）在这个文件里")
			ui.Text(c, "面板只展示最近 100 条；全部记录直接打开上面的文件查看").
				FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		}
		if len(entries) == 0 && connID != "" {
			ui.Text(c, "这个数据源还没有执行记录").TextColor(c.Theme().TextMuted)
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
		if iconButtonPlain(c, iconInsert, "插入到光标", ui.Transparent).Clicked() {
			a.code.Insert(e.SQL)
			a.historyOpen = false
		}
		if iconButtonPlain(c, iconCopy, "复制 SQL", ui.Transparent).Clicked() {
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
