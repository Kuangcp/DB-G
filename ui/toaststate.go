package ui

import "github.com/egoist/mygo/ui"

// toastState 是下一帧要弹出的浮窗提示队列。
type toastState struct {
	Queue []ui.Toast
	Seq   uint64
}
