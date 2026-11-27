package ui

import "github.com/egoist/mygo/ui"

// cellViewState 是单元格查看/编辑弹窗与 JSON 树的状态。
type cellViewState struct {
	Open          bool
	Title         string
	Text          string
	JSONOK        bool          // 当前内容按 JSON 树渲染
	JSONRoot      *jsonTreeNode // 解析后的 JSON 树
	JSONRows      []jsonTreeRow // 当前展开状态下的可见行
	JSONExpanded  map[string]bool
	JSONTruncated bool
	JSONList      ui.ListState
	FindOpen      bool
	FindQuery     string
	FindHits      []*jsonTreeNode
	FindCur       int
	FindLast      string
	Wrap          bool    // 长文本是否自动折行
	ModalW        float32 // 单元格查看/编辑弹窗宽度
	ModalH        float32 // 单元格查看/编辑弹窗高度
}
