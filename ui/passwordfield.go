package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// passwordField 是带眼睛按钮的单行密码框：眼睛叠在输入框内部右侧，
// 点击在隐藏与明文之间切换。*show 为非零值时直接显示明文。
func passwordField(c *ui.Context, value *string, show *bool, radius float32, placeholder, label string) ui.Element {
	t := c.Theme()
	padX := t.Space(2.5)
	var in ui.Element
	ui.Box(c).Fill().Children(func() {
		in = ui.TextInput(c, value).Placeholder(placeholder).Label(label).Radius(radius).Fill().
			Padding(t.Space(1.5), padX+26, t.Space(1.5), padX)
		if !*show {
			in.Password()
		}
		icon, tip := iconEye, locales.ConnFormShowPassword()
		if *show {
			icon, tip = iconEyeOff, locales.ConnFormHidePassword()
		}
		eye := iconButtonPlain(c, icon, tip, t.TextMuted).
			AttachTo(in, ui.AnchorRight, ui.AnchorRight).Right(6)
		if eye.Clicked() {
			*show = !*show
		}
	})
	return in
}
