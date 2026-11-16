package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// resultFooterHeight 是状态栏固定高度（含底边分隔线），让布局不随内容抖动。
const resultFooterHeight = 26

// runtimeTiming 返回当前结果 tab 的最近一次执行耗时；执行中/未执行返回空串。
func (a *root) runtimeTiming() string {
	rt := a.currentRuntime()
	if rt == nil || rt.Running() {
		return ""
	}
	total := rt.Dur()
	res := rt.Result()
	if total <= 0 && res == nil {
		return ""
	}
	if res != nil && res.IsQuery && res.ExecDuration > 0 {
		return fmt.Sprintf("耗时 %s（执行 %s · 读取 %s）",
			formatDuration(total), formatDuration(res.ExecDuration), formatDuration(res.FetchDuration))
	}
	if total <= 0 {
		return ""
	}
	return "耗时 " + formatDuration(total)
}

func formatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "0ms"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// resultFooter 是主窗口底栏：当前数据库、版本、数据源标签与最近一次执行耗时。
// 内容只读，常驻在主区域最底部。
func (a *root) resultFooter(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(1).FillWidth().Background(t.Border)
	ui.Row(c).FillWidth().Height(resultFooterHeight-1).AlignItems(ui.Center).Gap(10).Padding(4, 0).Children(func() {
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
		if timing := a.runtimeTiming(); timing != "" {
			ui.Text(c, timing).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
		}
		if !a.connected(connID) {
			ui.Text(c, "未连接").FontSize(11).TextColor(t.TextMuted)
		}
	})
}
