package ui

import "github.com/egoist/mygo/ui"

// controlRadius 是连接弹窗与设置窗口里按钮、输入框统一用的圆角：
// 比主题默认更圆一点（标准密度 8），并随密度缩放。
func controlRadius(c *ui.Context) float32 { return c.Theme().Space(2) }
