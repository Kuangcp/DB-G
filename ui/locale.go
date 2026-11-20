package ui

import "github.com/egoist/mygo/i18n"

// uiLocale 返回当前界面语言（BCP 47）。headless 测试里由 MYGO_LOCALE 决定，
// 不会像 App.Locale 那样等主线程。
func uiLocale() string { return i18n.Locale() }
