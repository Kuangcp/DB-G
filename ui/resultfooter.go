package ui

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// resultFooter 是结果区底栏：当前数据库、版本与数据源标签。
// 内容只读，始终贴在结果区最底部。
func (a *root) resultFooter(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(1).FillWidth().Background(t.Border)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(4, 0).Children(func() {
		connID := a.activeConnID()
		if connID == "" {
			ui.Text(c, "未连接数据源").FontSize(11.5).TextColor(t.TextMuted)
			return
		}
		var p app.ConnProfile
		if a.store != nil {
			p, _ = a.store.Get(connID)
		}
		info := a.serverInfo[connID]
		database := info.Database
		if database == "" {
			database = p.Database
			if database == "" && p.Kind == "sqlite" && p.Path != "" {
				database = filepath.Base(p.Path)
			}
		}
		if database != "" {
			ui.Text(c, "数据库").FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, database).FontSize(11.5).SingleLine()
		}
		switch {
		case info.Version != "":
			ui.Text(c, "版本").FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, info.Version).FontSize(11.5).SingleLine()
		case a.serverInfoErr[connID] != "":
			msg := ui.Text(c, "版本未知").FontSize(11).TextColor(t.TextMuted)
			msg.Tooltip(a.serverInfoErr[connID])
		case a.connected(connID):
			ui.Text(c, "版本读取中…").FontSize(11).TextColor(t.TextMuted)
		}
		for _, tag := range p.Tags {
			tagChip(c, tag)
		}
		ui.Spacer(c)
		if !a.connected(connID) {
			ui.Text(c, "未连接").FontSize(11).TextColor(t.TextMuted)
		}
	})
}
