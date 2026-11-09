package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// guardModal 展示高危 SQL 风险；硬阻断连接不提供“仍然执行”。
func (a *root) guardModal(c *ui.Context) {
	if !a.guardOpen {
		return
	}
	t := c.Theme()
	title := "高危 SQL 提醒"
	if a.guardBlocked {
		title = "已阻断高危 SQL"
	}
	ui.Modal(c, &a.guardOpen, func() {
		ui.Text(c, title).Bold().TextColor(t.Danger)
		ui.Text(c, "以下规则由本地确定性检查命中，不依赖 AI。").
			FontSize(11).TextColor(t.TextMuted)
		ui.Column(c).Gap(4).Children(func() {
			for _, risk := range a.guardRisks {
				msg := risk.Message
				if risk.Rule != "" {
					msg = fmt.Sprintf("[%s] %s", risk.Rule, msg)
				}
				ui.Text(c, msg).FontSize(12).TextColor(t.Danger)
			}
		})
		if a.guardBlocked {
			ui.Text(c, "该连接已开启「高危 SQL 直接阻断」，请修改 SQL 后重试。").
				FontSize(12).TextColor(t.TextMuted)
		} else {
			ui.Text(c, "确认要执行吗？建议先补齐 WHERE 或改用 SELECT 验证。").
				FontSize(12).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.guardOpen = false
			}
			if !a.guardBlocked {
				if ui.PrimaryButton(c, "仍然执行").Clicked() {
					a.guardOpen = false
					app.LogWarn("exec", "risky sql confirmed", "rules", guardRules(a.guardRisks))
					a.runSQLNow()
				}
			}
		})
	}).Width(560).Height(300)
}

func guardRules(risks []app.SQLRisk) string {
	out := ""
	for i, r := range risks {
		if i > 0 {
			out += ","
		}
		out += r.Rule
	}
	return out
}
