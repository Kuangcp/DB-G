package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// showAILintButton 是 AI 面板/修复/改写里的一行自检入口；点击打开完整报告。
func showAILintButton(a *root, c *ui.Context, rep aiSQLReport) {
	t := c.Theme()
	color := t.Warning
	if rep.HasRisk() {
		color = t.Danger
	}
	text := truncateRunes(rep.Summary(), 80)
	if ui.Button(c, text).FontSize(10.5).TextColor(color).Tooltip(rep.Summary()).Clicked() {
		a.ai.LintReport = rep
		a.ai.LintOpen = true
	}
}

// aiLintText 是复制/展示用的完整报告。
func aiLintText(rep aiSQLReport) string {
	var out string
	if rep.Summary() != "" {
		out = rep.Summary() + "\n\n"
	}
	out += rep.SQL
	return out
}

// aiLintModal 展示最近一次 AI SQL 自检的完整问题与原文。
func (a *root) aiLintModal(c *ui.Context) {
	if !a.ai.LintOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &a.ai.LintOpen, func() {
		ui.Text(c, locales.AiSelfCheckTitle()).Bold()
		ui.Scroll(c).Height(190).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, risk := range a.ai.LintReport.Risks {
					row := ui.Text(c, risk.Message).FontSize(12).TextColor(t.Danger).SingleLine()
					if risk.Rule != "" {
						row.Tooltip(risk.Rule)
					}
				}
				for _, issue := range a.ai.LintReport.Issues {
					var text string
					if issue.Column == "" {
						text = locales.AiSelfCheckUnknownTable(issue.Table)
					} else {
						text = locales.AiSelfCheckUnknownColumn(issue.Column, issue.Table)
					}
					ui.Text(c, text).FontSize(12).TextColor(t.Warning).SingleLine()
				}
			})
		})
		ui.Text(c, locales.AiSelfCheckSQL()).FontSize(11).TextColor(t.TextMuted)
		ui.TextArea(c, &a.ai.LintReport.SQL).ReadOnly(true).Lines(8, 12).FillWidth()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.AiSelfCheckCopy()).Clicked() {
				c.WriteClipboard(aiLintText(a.ai.LintReport))
			}
			if ui.PrimaryButton(c, locales.CommonClose()).Clicked() {
				a.ai.LintOpen = false
			}
		})
	}).Width(680).Height(500)
}
