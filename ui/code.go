package ui

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"
)

// codeEditor 包住 ui/editor：每控制台一个 Buffer（切换零拷贝、撤销历史独立），
// 光标在 rune（持久化语义）与字节（编辑器语义）之间只在激活/读取时换算。
type codeEditor struct {
	state   *myeditor.State
	buffers map[string]*myeditor.Buffer
	active  string
}

// newCodeEditor 创建编辑器封装；placeholder 在正文为空时显示。
func newCodeEditor(placeholder string) *codeEditor {
	return &codeEditor{
		state: &myeditor.State{
			// Ctrl+Enter 交给窗口做「执行选中」：NoCommand 让编辑器不消费它。
			Bindings: []myeditor.Binding{
				{Mods: ui.Ctrl, Key: ui.KeyEnter, Command: myeditor.NoCommand},
			},
			Placeholder: placeholder,
		},
		buffers: map[string]*myeditor.Buffer{},
	}
}

// SetActive 把编辑器切到该控制台的正文（同一控制台复用它的 Buffer）。
func (c *codeEditor) SetActive(consoleID, text string) {
	buf, ok := c.buffers[consoleID]
	switch {
	case !ok:
		buf = myeditor.NewBuffer(text)
		buf.SetLanguage(myeditor.LanguageNamed("SQL"))
		c.buffers[consoleID] = buf
	case buf.String() != text:
		// 只有正文真的不同（外部改动/首次载入）才 Reset；否则保留撤销历史。
		buf.Reset(text)
	}
	c.state.Buffer = buf
	c.active = consoleID
}

// Clear 进入「无激活控制台」空态（保留各控制台的 Buffer）。
func (c *codeEditor) Clear() {
	c.state.Buffer = myeditor.NewBuffer("")
	c.active = ""
}

// Drop 丢弃某控制台的 Buffer（控制台删除时）；丢掉的是当前控制台则进入空态。
func (c *codeEditor) Drop(consoleID string) {
	delete(c.buffers, consoleID)
	if c.active == consoleID {
		c.Clear()
	}
}

// Text 返回当前正文。
func (c *codeEditor) Text() string {
	if c.state.Buffer == nil {
		return ""
	}
	return c.state.Buffer.String()
}

// SetText 整体替换正文（Reset：不产生撤销步，正如切换控制台）。
func (c *codeEditor) SetText(text string) {
	if c.state.Buffer == nil {
		c.state.Buffer = myeditor.NewBuffer(text)
		return
	}
	c.state.Buffer.Reset(text)
}

// Version 返回正文版本号（逐帧比较用，常数级）。
func (c *codeEditor) Version() uint64 {
	if c.state.Buffer == nil {
		return 0
	}
	return c.state.Buffer.Version()
}

// Selection 返回选区（字节偏移）；无选区时 start == end。
func (c *codeEditor) Selection() (start, end int, hasSel bool) {
	sel := c.state.Selection()
	return sel.Start(), sel.End(), !sel.Empty()
}

// SetSelection 用 rune 偏移设置选区/光标（持久化语义），内部换算成字节。
func (c *codeEditor) SetSelection(startRune, endRune int) {
	text := c.Text()
	c.state.Select(runeToByte(text, startRune), runeToByte(text, endRune))
}

// CaretRunes 返回光标/选区的 rune 偏移（有序）。
func (c *codeEditor) CaretRunes() (start, end int) {
	text := c.Text()
	start, end, _ = c.Selection()
	return byteToRune(text, start), byteToRune(text, end)
}

// SelectedText 返回选中的 SQL；无选区返回 false。
func (c *codeEditor) SelectedText() (string, bool) {
	if c.state.Selection().Empty() {
		return "", false
	}
	return c.state.SelectedText(), true
}

// Replace 用 text 替换字节范围 [start,end)（进入撤销栈）。
func (c *codeEditor) Replace(start, end int, text string) {
	c.state.Select(start, end)
	c.state.Insert(text)
}

// Insert 在光标处输入文本。
func (c *codeEditor) Insert(text string) { c.state.Insert(text) }

// Find 打开编辑器的查找栏。
func (c *codeEditor) Find() { c.state.Find("") }

// LineCount 返回行数（空文档为 1）。
func (c *codeEditor) LineCount() int {
	if c.state.Buffer == nil {
		return 0
	}
	return c.state.Buffer.LineCount()
}

// ByteLen 返回正文的字节长度。
func (c *codeEditor) ByteLen() int {
	if c.state.Buffer == nil {
		return 0
	}
	return c.state.Buffer.Len()
}

// View 构建编辑器元素。
func (c *codeEditor) View(cx *ui.Context) ui.Element {
	return myeditor.View(cx, c.state).Grow(1).FillWidth()
}

// runeToByte 把 rune 偏移换成字节偏移（越界夹紧）。
func runeToByte(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	off, n := 0, 0
	for off < len(s) && n < runeIdx {
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
		n++
	}
	return off
}

// byteToRune 把字节偏移换成 rune 偏移（越界夹紧，落在字符中间时回到该字符起点）。
func byteToRune(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff > len(s) {
		byteOff = len(s)
	}
	for byteOff > 0 && byteOff < len(s) && !utf8.RuneStart(s[byteOff]) {
		byteOff--
	}
	return utf8.RuneCountInString(s[:byteOff])
}
