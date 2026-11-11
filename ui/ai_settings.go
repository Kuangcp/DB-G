package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/app"
)

// aiModelsTimeout 是 /models 请求的超时；失败就退回手动输入模型名。
const aiModelsTimeout = 8 * time.Second

// aiSection 是设置窗口的 AI 分区：先填服务地址与密钥，再选模型。
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
		a.invalidateAIModels()
	}
	ui.Text(c, "Ollama 填 http://127.0.0.1:11434/v1；OpenAI/DeepSeek 填对应 /v1 地址。").FontSize(11).
		TextColor(c.Theme().TextMuted)

	ui.Text(c, "API Key（本地 Ollama 可留空）").FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.aiPrefs.APIKey).
		Placeholder("sk-…").
		Label("AI API Key").
		Password().
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
		a.invalidateAIModels()
	}
	ui.Text(c, "密钥用本机 secret.key 加密后写入 ai.json，不会进入日志。").FontSize(11).
		TextColor(c.Theme().TextMuted)

	// 地址与密钥就绪后自动拉一次模型列表；失败/未返回时保留手动输入。
	a.maybeFetchAIModels()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "模型").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		switch {
		case a.aiModelsLoading:
			ui.Spinner(c)
			ui.Text(c, "正在获取模型列表…").FontSize(11).TextColor(c.Theme().TextMuted)
		case a.aiModelsErr != "":
			ui.Text(c, "获取失败，可手动输入").FontSize(11).TextColor(c.Theme().Warning).
				SingleLine().Tooltip(a.aiModelsErr)
		case len(a.aiModels) > 0:
			ui.Text(c, "可直接选择").FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if iconButtonPlain(c, iconRefresh, "获取模型列表", ui.Transparent).
			Disabled(a.aiModelsLoading || strings.TrimSpace(a.aiPrefs.BaseURL) == "").Clicked() {
			a.fetchAIModels()
		}
	})
	options := a.aiModelOptions()
	if len(options) > 0 {
		if ui.Combobox(c, &a.aiPrefs.Model, options).
			Placeholder("选择模型").
			Label("AI 模型").
			Radius(cr).
			Fill().
			Changed() {
			a.saveAIPrefs()
		}
	} else if ui.TextInput(c, &a.aiPrefs.Model).
		Placeholder("例如 qwen2.5-coder:7b / deepseek-chat").
		Label("AI 模型").
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
	}

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

// invalidateAIModels 丢弃旧的模型列表；地址/密钥改动后重新自动获取。
func (a *root) invalidateAIModels() {
	a.aiModelsSeq++
	a.aiModels, a.aiModelsKey, a.aiModelsErr = nil, "", ""
	a.aiModelsLoading = false
}

// aiModelsFingerprint 标识一次模型列表请求；密钥不会出现在日志里。
func (a *root) aiModelsFingerprint() string {
	return strings.TrimSpace(a.aiPrefs.BaseURL) + "\x00" + a.aiPrefs.APIKey
}

// aiCanFetchModels 报告当前配置是否值得请求 /models：填了密钥，
// 或者指向本机 Ollama（本地服务通常不需要密钥）。
func (a *root) aiCanFetchModels() bool {
	base := strings.TrimSpace(a.aiPrefs.BaseURL)
	if base == "" {
		return false
	}
	if strings.TrimSpace(a.aiPrefs.APIKey) != "" {
		return true
	}
	return strings.Contains(base, "127.0.0.1") || strings.Contains(base, "localhost") ||
		strings.Contains(base, "[::1]")
}

// maybeFetchAIModels 在设置页首次显示、或地址/密钥改完后自动拉一次列表。
func (a *root) maybeFetchAIModels() {
	if a.win == nil && a.aiTestProvider == nil {
		return // headless 测试不主动访问网络
	}
	if a.aiModelsLoading || len(a.aiModels) > 0 || a.aiModelsErr != "" {
		return
	}
	if !a.aiCanFetchModels() || a.aiModelsKey == a.aiModelsFingerprint() {
		return
	}
	a.fetchAIModels()
}

// aiModelOptions 返回下拉选项；把当前手填的模型也放进去，避免被列表吞掉。
func (a *root) aiModelOptions() []string {
	opts := append([]string(nil), a.aiModels...)
	if m := strings.TrimSpace(a.aiPrefs.Model); m != "" && !slices.Contains(opts, m) {
		opts = append([]string{m}, opts...)
	}
	return opts
}

// fetchAIModels 通过 OpenAI 协议的 GET /models 获取模型列表；失败时 UI 退回手动输入。
func (a *root) fetchAIModels() {
	base := strings.TrimSpace(a.aiPrefs.BaseURL)
	if base == "" {
		a.aiModelsErr = "请先填写服务地址"
		return
	}
	key := a.aiModelsFingerprint()
	apiKey := a.aiPrefs.APIKey
	testProvider := a.aiTestProvider
	a.aiModelsSeq++
	seq := a.aiModelsSeq
	a.aiModelsKey = key
	a.aiModelsLoading, a.aiModelsErr = true, ""
	run := func() ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), aiModelsTimeout)
		defer cancel()
		if testProvider != nil {
			return testProvider.Models(ctx)
		}
		provider := ai.NewOpenAIProvider(ai.OpenAIConfig{BaseURL: base, APIKey: apiKey})
		return provider.Models(ctx)
	}
	apply := func(models []string, err error) {
		if a.aiModelsSeq != seq {
			return
		}
		a.aiModelsLoading = false
		if err != nil {
			a.aiModels, a.aiModelsErr = nil, err.Error()
			return
		}
		a.aiModels, a.aiModelsErr = models, ""
		if len(models) == 0 {
			a.aiModelsErr = "服务未返回模型列表"
		}
	}
	if a.win == nil {
		models, err := run()
		apply(models, err)
		return
	}
	win := a.win
	go func() {
		models, err := run()
		a.safeUpdate(win, func() {
			apply(models, err)
			a.updateSettingsWindow()
		})
	}()
}
