package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// aiSection 是设置窗口的 AI 分区。
func (a *root) aiSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "AI 助手").Bold()
		ui.Spacer(c)
		ui.Text(c, "默认本地 Ollama，也可填任意 OpenAI 兼容服务").FontSize(11).TextColor(c.Theme().TextMuted)
	})

	ui.Text(c, "服务地址").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.aiPrefs.BaseURL).
		Placeholder(app.DefaultAIBaseURL).
		Label("AI 服务地址").
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
	}
	ui.Text(c, "Ollama 填 http://127.0.0.1:11434/v1；OpenAI/DeepSeek 填对应 /v1 地址。").FontSize(11).
		TextColor(c.Theme().TextMuted)

	ui.Text(c, "模型").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.aiPrefs.Model).
		Placeholder("例如 qwen2.5-coder:7b / deepseek-chat").
		Label("AI 模型").
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
	}

	ui.Text(c, "API Key（本地 Ollama 可留空）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.aiPrefs.APIKey).
		Placeholder("sk-…").
		Label("AI API Key").
		Password().
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
	}
	ui.Text(c, "密钥用本机 secret.key 加密后写入 ai.json，不会进入日志。").FontSize(11).
		TextColor(c.Theme().TextMuted)

	ui.Divider(c)
	ui.Text(c, "默认只把表结构和当前 SQL 发给模型；下面三项按需开启。").FontSize(12).
		TextColor(c.Theme().TextMuted)
	if ui.Checkbox(c, &a.aiPrefs.AllowHistory, "允许发送最近查询历史").Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.aiPrefs.AllowErrorLogs, "允许发送最近执行错误").Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.aiPrefs.AllowSampleRows, "允许发送表样本行（第一期尚未采集）").Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.aiPrefs.AITitle, "用 AI 生成会话标题（额外一次调用，默认关）").Changed() {
		a.saveAIPrefs()
	}
	if a.aiWarn != "" {
		ui.Text(c, a.aiWarn).FontSize(11).TextColor(c.Theme().Danger).SingleLine().Tooltip(a.aiWarn)
	}
}
