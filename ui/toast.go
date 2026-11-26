package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/locales"
)

// toastTimeout 是浮窗提示的展示时长：到点自动消失，悬停时暂停计时。
const toastTimeout = 4 * time.Second

// toastError 让下一帧在窗口底部弹出一条错误提示（自动消失）。
// 网络/建连这类发生在后台、且不属于当前控制台的错误走这里，不要占顶栏。
func (a *root) toastError(msg string) {
	if msg == "" {
		return
	}
	a.toastSeq++
	a.toastQueue = append(a.toastQueue, ui.Toast{
		ID:      fmt.Sprintf("dbg-toast-%d", a.toastSeq),
		Title:   msg,
		Type:    "error",
		Timeout: toastTimeout,
	})
}

// flushToasts 在帧首把排队的提示交给 mygo 的 toast 视口。
func (a *root) flushToasts(c *ui.Context) {
	for _, t := range a.toastQueue {
		c.AddToast(t)
	}
	a.toastQueue = nil
}

// toastViewport 是浮窗提示的视口：底部居中、单行、错误用 Danger 描边，
// 右侧可点关闭；展示时长由 Toast.Timeout 决定（mygo 计时，悬停暂停）。
// 视口只在本函数里建一次，替代 mygo 的默认外观，颜色全部取主题。
func (a *root) toastViewport(c *ui.Context) {
	t := c.Theme()
	w, _ := c.Size()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		if len(toasts) == 0 {
			return
		}
		viewport.Bottom(t.Space(6)).AlignItems(ui.Center).Gap(t.Space(2))
		for _, msg := range toasts {
			parts := ui.ToastBase(c, msg)
			edge := t.Border
			if msg.Type == "error" {
				edge = t.Danger
			}
			box := parts.Root.Row().AlignItems(ui.Center).Gap(t.Space(3)).
				Padding(t.Space(2), t.Space(4)).Radius(t.Space(2)).
				MaxWidth(w-t.Space(12)).Background(t.Surface).Border(1, edge)
			box.Children(func() {
				ui.Text(c, msg.Title).FontSize(12.5).TextColor(t.Text).
					SingleLine().Tooltip(msg.Title)
				close := parts.CloseButton().Label(locales.CommonClose()).
					Tooltip(locales.CommonClose()).Padding(t.Space(1)).Radius(t.Space(1))
				close.Children(func() { iconChild(c, iconClose, t.TextMuted) })
				if close.Hovered() {
					close.Background(t.SurfaceHover)
				}
			})
		}
	})
}
