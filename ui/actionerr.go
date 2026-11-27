package ui

import "github.com/egoist/mygo/ui"

// settingsNotice 是设置窗口里的内联提示；err 决定是否用 Danger 色。
type settingsNotice struct {
	text string
	err  bool
}

// setSettingsNotice 写/清一条设置面板内联提示（key 对应分区，如 "keymap"、"profiles"）。
func (a *root) setSettingsNotice(key, text string, isErr bool) {
	if key == "" {
		return
	}
	if text == "" {
		delete(a.settingsNotice, key)
		return
	}
	if a.settingsNotice == nil {
		a.settingsNotice = map[string]settingsNotice{}
	}
	a.settingsNotice[key] = settingsNotice{text: text, err: isErr}
}

// reportSettingsResult 是设置窗口操作结果的统一出口：内联提示 + 失败时 toast，
// 绝不写主窗口顶栏（顶栏只属于当前控制台的执行错误）。
func (a *root) reportSettingsResult(key, text string, isErr bool) {
	a.setSettingsNotice(key, text, isErr)
	if isErr {
		a.toastError(text)
	}
}

// settingsNoticeOf 返回某个设置分区的内联提示。
func (a *root) settingsNoticeOf(key string) (settingsNotice, bool) {
	n, ok := a.settingsNotice[key]
	return n, ok && n.text != ""
}

// reportBackgroundError 用于不属于当前控制台、也没有面板可落的错误。
func (a *root) reportBackgroundError(msg string) {
	a.toastError(msg)
}

// settingsNoticeLine 在设置分区的固定位置画一条内联提示（错误红色、成功用次要色）。
func (a *root) settingsNoticeLine(c *ui.Context, key string) {
	n, ok := a.settingsNoticeOf(key)
	if !ok {
		return
	}
	color := c.Theme().TextMuted
	if n.err {
		color = c.Theme().Danger
	}
	ui.Text(c, n.text).FontSize(12).TextColor(color).SingleLine().Tooltip(n.text)
}
