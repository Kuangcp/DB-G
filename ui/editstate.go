package ui

import (
	"sync/atomic"

	"db-g/app"
)

// editState 是结果网格的编辑集、内联编辑与提交/预览弹窗状态。
type editState struct {
	Sets      map[string]*app.EditSet
	Busy      bool
	Status    string
	StatusSeq atomic.Uint64

	CellOpen bool
	CellRow  int
	CellCol  int
	CellText string

	InlineOn       bool
	InlineRow      int
	InlineCol      int
	InlineText     string
	InlineHadFocus bool

	PreviewOpen  bool
	PreviewLines []string
}
