package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/app"
	"db-g/locales"
)

// aiModelsTimeout 是 /models 请求的超时；失败就退回手动输入模型名。
const aiModelsTimeout = 8 * time.Second

// aiSection 是设置窗口的 AI 分区：先填服务地址与密钥，再选模型。
func (a *root) aiSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiAssistant()).Bold()
		ui.Spacer(c)
		ui.Text(c, locales.AiSettingsHint()).FontSize(11).TextColor(c.Theme().TextMuted)
	})

	ui.Text(c, locales.AiBaseURL()).FontSize(12).TextColor(c.Theme().TextMuted)
	if ui.TextInput(c, &a.ai.Prefs.BaseURL).
		Placeholder(app.DefaultAIBaseURL).
		Label(locales.AiBaseURLLabel()).
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
		a.invalidateAIModels()
	}
	ui.Text(c, locales.AiBaseURLHint()).FontSize(11).
		TextColor(c.Theme().TextMuted)

	ui.Text(c, locales.AiAPIKey()).FontSize(12).TextColor(c.Theme().TextMuted)
	if passwordField(c, &a.ai.Prefs.APIKey, &a.ai.ShowKey, cr, "sk-…", "AI API Key").Changed() {
		a.saveAIPrefs()
		a.invalidateAIModels()
	}
	ui.Text(c, locales.AiAPIKeyHint()).FontSize(11).
		TextColor(c.Theme().TextMuted)

	// 地址与密钥就绪后自动拉一次模型列表；失败/未返回时保留手动输入。
	a.maybeFetchAIModels()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiModel()).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		switch {
		case a.ai.ModelsLoading:
			ui.Spinner(c)
			ui.Text(c, locales.AiModelsLoading()).FontSize(11).TextColor(c.Theme().TextMuted)
		case a.ai.ModelsErr != "":
			ui.Text(c, locales.AiModelsFailed()).FontSize(11).TextColor(c.Theme().Warning).
				SingleLine().Tooltip(a.ai.ModelsErr)
		case len(a.ai.Models) > 0:
			ui.Text(c, locales.AiModelsReady()).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if iconButtonPlain(c, iconRefresh, locales.AiFetchModels(), ui.Transparent).
			Disabled(a.ai.ModelsLoading || strings.TrimSpace(a.ai.Prefs.BaseURL) == "").Clicked() {
			a.fetchAIModels()
		}
	})
	options := a.aiModelOptions()
	if len(options) > 0 {
		if ui.Combobox(c, &a.ai.Prefs.Model, options).
			Placeholder(locales.AiSelectModel()).
			Label(locales.AiModelLabel()).
			Radius(cr).
			Fill().
			Changed() {
			a.saveAIPrefs()
		}
	} else if ui.TextInput(c, &a.ai.Prefs.Model).
		Placeholder(locales.AiModelPlaceholder()).
		Label(locales.AiModelLabel()).
		Radius(cr).
		Changed() {
		a.saveAIPrefs()
	}

	// 任务级模型：留空继承聊天模型。
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiCompletionModel()).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Width(130)
		ui.Spacer(c)
		if ui.TextInput(c, &a.ai.Prefs.CompletionModel).
			Placeholder(locales.AiModelInherit()).Label(locales.AiCompletionModel()).
			Radius(cr).Fill().Changed() {
			a.saveAIPrefs()
		}
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiFixModel()).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Width(130)
		ui.Spacer(c)
		if ui.TextInput(c, &a.ai.Prefs.FixModel).
			Placeholder(locales.AiModelInherit()).Label(locales.AiFixModel()).
			Radius(cr).Fill().Changed() {
			a.saveAIPrefs()
		}
	})

	ui.Divider(c)
	ui.Text(c, locales.AiTaskParams()).Bold()
	ui.Text(c, locales.AiTaskParamsHint()).FontSize(11).TextColor(c.Theme().TextMuted)
	taskNum := func(label string, v *int, lo, hi, step int) {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Width(160)
			ui.Spacer(c)
			n := float64(*v)
			if ui.NumberInput(c, &n, float64(lo), float64(hi), float64(step)).
				Label(label).Width(110).Radius(cr).Changed() {
				*v = int(n)
				a.saveAIPrefs()
			}
		})
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiChatTemperature()).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Width(160)
		ui.Spacer(c)
		temp := a.ai.Prefs.ChatTemperature
		if ui.NumberInput(c, &temp, 0.1, 2, 0.1).
			Label(locales.AiChatTemperature()).Width(110).Radius(cr).Changed() {
			a.ai.Prefs.ChatTemperature = temp
			a.saveAIPrefs()
		}
	})
	taskNum(locales.AiChatMaxTokens(), &a.ai.Prefs.ChatMaxTokens, 256, 32768, 64)
	taskNum(locales.AiCompletionMaxTokens(), &a.ai.Prefs.CompletionMaxTokens, 32, 4096, 32)
	taskNum(locales.AiFixMaxTokens(), &a.ai.Prefs.FixMaxTokens, 256, 8192, 64)
	taskNum(locales.AiRewriteMaxTokens(), &a.ai.Prefs.RewriteMaxTokens, 256, 8192, 64)

	// 上下文窗口只影响提示词裁剪，不会改模型本身；0 表示用默认 8192。
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiContextWindow()).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Width(160)
		win := float64(a.ai.Prefs.ContextWindow)
		if win <= 0 {
			win = float64(ai.DefaultContextWindow)
		}
		if ui.NumberInput(c, &win, 2048, 131072, 1024).
			Label(locales.AiContextWindowLabel()).Width(110).Radius(cr).Changed() {
			a.ai.Prefs.ContextWindow = int(win)
			a.saveAIPrefs()
		}
		ui.Text(c, locales.AiContextWindowValue(ai.DefaultContextWindow)).FontSize(11).
			TextColor(c.Theme().TextMuted)
	})
	ui.Text(c, locales.AiContextWindowHint()).FontSize(11).TextColor(c.Theme().TextMuted)

	ui.Divider(c)
	ui.Text(c, locales.AiPrivacyHint()).FontSize(12).
		TextColor(c.Theme().TextMuted)
	if ui.Checkbox(c, &a.ai.Prefs.AllowHistory, locales.AiAllowHistory()).Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.ai.Prefs.AllowErrorLogs, locales.AiAllowErrors()).Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.ai.Prefs.AllowSampleRows, locales.AiAllowSamples()).Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.ai.Prefs.AllowDDL, locales.AiAllowDDL()).Changed() {
		a.saveAIPrefs()
	}
	if ui.Checkbox(c, &a.ai.Prefs.AITitle, locales.AiAutoTitle()).Changed() {
		a.saveAIPrefs()
	}
	if a.ai.Warn != "" {
		ui.Text(c, a.ai.Warn).FontSize(11).TextColor(c.Theme().Danger).SingleLine().Tooltip(a.ai.Warn)
	}
}

// invalidateAIModels 丢弃旧的模型列表；地址/密钥改动后重新自动获取。
func (a *root) invalidateAIModels() {
	a.ai.ModelsSeq++
	a.ai.Models, a.ai.ModelsKey, a.ai.ModelsErr = nil, "", ""
	a.ai.ModelsLoading = false
}

// aiModelsFingerprint 标识一次模型列表请求；密钥不会出现在日志里。
func (a *root) aiModelsFingerprint() string {
	return strings.TrimSpace(a.ai.Prefs.BaseURL) + "\x00" + a.ai.Prefs.APIKey
}

// aiCanFetchModels 报告当前配置是否值得请求 /models：填了密钥，
// 或者指向本机 Ollama（本地服务通常不需要密钥）。
func (a *root) aiCanFetchModels() bool {
	base := strings.TrimSpace(a.ai.Prefs.BaseURL)
	if base == "" {
		return false
	}
	if strings.TrimSpace(a.ai.Prefs.APIKey) != "" {
		return true
	}
	return strings.Contains(base, "127.0.0.1") || strings.Contains(base, "localhost") ||
		strings.Contains(base, "[::1]")
}

// maybeFetchAIModels 在设置页首次显示、或地址/密钥改完后自动拉一次列表。
func (a *root) maybeFetchAIModels() {
	if a.win == nil && a.ai.TestProvider == nil {
		return // headless 测试不主动访问网络
	}
	if a.ai.ModelsLoading || len(a.ai.Models) > 0 || a.ai.ModelsErr != "" {
		return
	}
	if !a.aiCanFetchModels() || a.ai.ModelsKey == a.aiModelsFingerprint() {
		return
	}
	a.fetchAIModels()
}

// aiModelOptions 返回下拉选项；把当前手填的模型也放进去，避免被列表吞掉。
func (a *root) aiModelOptions() []string {
	opts := append([]string(nil), a.ai.Models...)
	if m := strings.TrimSpace(a.ai.Prefs.Model); m != "" && !slices.Contains(opts, m) {
		opts = append([]string{m}, opts...)
	}
	return opts
}

// fetchAIModels 通过 OpenAI 协议的 GET /models 获取模型列表；失败时 UI 退回手动输入。
func (a *root) fetchAIModels() {
	base := strings.TrimSpace(a.ai.Prefs.BaseURL)
	if base == "" {
		a.ai.ModelsErr = locales.AiBaseURLRequired()
		return
	}
	key := a.aiModelsFingerprint()
	apiKey := a.ai.Prefs.APIKey
	testProvider := a.ai.TestProvider
	a.ai.ModelsSeq++
	seq := a.ai.ModelsSeq
	a.ai.ModelsKey = key
	a.ai.ModelsLoading, a.ai.ModelsErr = true, ""
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
		if a.ai.ModelsSeq != seq {
			return
		}
		a.ai.ModelsLoading = false
		if err != nil {
			a.ai.Models, a.ai.ModelsErr = nil, err.Error()
			return
		}
		a.ai.Models, a.ai.ModelsErr = models, ""
		if len(models) == 0 {
			a.ai.ModelsErr = locales.AiNoModelsReturned()
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
