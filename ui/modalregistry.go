package ui

import "github.com/egoist/mygo/ui"

// modalRegistry 是每帧按顺序询问「要不要画」的弹窗列表。
// 新增弹窗只需在 newRoot 里注册（或测试里 add），不再改 view 的手写调用列表。
type modalRegistry struct {
	drawFns []func(*ui.Context)
}

// add 追加弹窗绘制函数；nil 忽略。
func (r *modalRegistry) add(fns ...func(*ui.Context)) {
	for _, fn := range fns {
		if fn != nil {
			r.drawFns = append(r.drawFns, fn)
		}
	}
}

// draw 按注册顺序绘制当前激活的弹窗；没有激活的弹窗函数会自行早退。
func (r *modalRegistry) draw(c *ui.Context) {
	for _, fn := range r.drawFns {
		fn(c)
	}
}

// len 返回注册的弹窗数量（测试/诊断用）。
func (r *modalRegistry) len() int { return len(r.drawFns) }
