package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// guardModal 展示高危 SQL 风险；硬阻断连接不提供“仍然执行”。
func (a *root) guardModal(c *ui.Context) {
	if !a.modal.GuardOpen {
		return
	}
	t := c.Theme()
	title := locales.GuardTitle()
	if a.modal.GuardBlocked {
		title = locales.GuardBlockedTitle()
	}
	ui.Modal(c, &a.modal.GuardOpen, func() {
		ui.Text(c, title).Bold().TextColor(t.Danger)
		ui.Text(c, locales.GuardExplain()).
			FontSize(11).TextColor(t.TextMuted)
		ui.Column(c).Gap(4).Children(func() {
			for _, risk := range a.modal.GuardRisks {
				msg := risk.Message
				if risk.Rule != "" {
					msg = fmt.Sprintf("[%s] %s", risk.Rule, msg)
				}
				ui.Text(c, msg).FontSize(12).TextColor(t.Danger)
			}
		})
		if a.modal.GuardBlocked {
			ui.Text(c, locales.GuardBlockedBody()).
				FontSize(12).TextColor(t.TextMuted)
		} else {
			ui.Text(c, locales.GuardConfirmBody()).
				FontSize(12).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.modal.GuardOpen = false
			}
			if !a.modal.GuardBlocked {
				if ui.PrimaryButton(c, locales.GuardRunAnyway()).Clicked() {
					a.modal.GuardOpen = false
					app.LogWarn("exec", "risky sql confirmed", "rules", guardRules(a.modal.GuardRisks))
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
