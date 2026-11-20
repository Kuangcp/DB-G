package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/i18n"
)

// TestMain 固定 app 测试的界面语言为简体中文；错误与警告文案在测试里按中文断言。
// app 包不 import mygo 根包，所以这里自己注册 locale 解析函数。
func TestMain(m *testing.M) {
	lang := os.Getenv("MYGO_LOCALE")
	if lang == "" {
		lang = "zh-Hans"
	}
	i18n.SetLocaleFunc(func() string { return lang })
	os.Exit(m.Run())
}
