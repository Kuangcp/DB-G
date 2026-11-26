package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"
)

func newTestCode() *codeEditor { return newCodeEditor("在左侧数据源下新建或打开控制台") }

func TestCodeEditorRuneByteRoundTrip(t *testing.T) {
	c := newTestCode()
	text := "SELECT '你好😀' AS s" // 含 CJK 与 4 字节 emoji
	c.SetActive("c1", text)
	for _, runePos := range []int{0, 7, 8, 10, 14, len([]rune(text))} {
		c.SetSelection(runePos, runePos)
		if s, e := c.CaretRunes(); s != runePos || e != runePos {
			t.Fatalf("caret %d round trip → %d,%d", runePos, s, e)
		}
	}
	c.SetSelection(8, 10) // '你好'
	if got, ok := c.SelectedText(); !ok || got != "你好" {
		t.Fatalf("selected text = %q ok=%v", got, ok)
	}
	if s, e, has := c.Selection(); !has || s != runeToByte(text, 8) || e != runeToByte(text, 10) {
		t.Fatalf("byte selection = %d,%d", s, e)
	}
}

func TestCodeEditorPerConsoleBuffers(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 'A'")
	c.SetActive("b", "SELECT 'B'")
	if len(c.buffers) != 2 {
		t.Fatalf("each console keeps its own buffer: %d", len(c.buffers))
	}
	c.SetActive("a", "SELECT 'A'")
	if got := c.Text(); got != "SELECT 'A'" {
		t.Fatalf("buffer per console: %q", got)
	}
	c.SetActive("b", "SELECT 'B'")
	if got := c.Text(); got != "SELECT 'B'" {
		t.Fatalf("buffer per console: %q", got)
	}
}

func TestCodeEditorResetHasNoUndoButTypingDoes(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.SetText("SELECT 2")
	if c.state.Buffer.CanUndo() {
		t.Fatal("SetText/Reset must not create an undo step")
	}
	c.Insert("3")
	if !c.state.Buffer.CanUndo() {
		t.Fatal("typing must create an undo step")
	}
}

func TestCodeEditorEmptyState(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.Clear()
	if c.Text() != "" || c.ByteLen() != 0 {
		t.Fatalf("clear: text=%q bytes=%d", c.Text(), c.ByteLen())
	}
	if _, _, has := c.Selection(); has {
		t.Fatal("empty state must have no selection")
	}
	if _, ok := c.SelectedText(); ok {
		t.Fatal("empty state has no selected text")
	}
	// 清空后仍可重新激活原来的控制台（Buffer 未丢）
	c.SetActive("a", "SELECT 1")
	if got := c.Text(); got != "SELECT 1" {
		t.Fatalf("reactivate after clear: %q", got)
	}
}

func TestCodeEditorDropForgetsBuffer(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.Drop("a")
	if len(c.buffers) != 0 {
		t.Fatalf("drop must forget the buffer: %d", len(c.buffers))
	}
	if c.active != "" || c.Text() != "" {
		t.Fatalf("dropping the active console clears the editor: active=%q", c.active)
	}
}

func TestCtrlEnterIsTakenFromTheEditor(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	tt := ui.NewTester(func(ctx *ui.Context) {
		c.View(ctx).AutoFocus()
	}, 800, 400)
	tt.Frame()
	before := c.Text()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if got := c.Text(); got != before {
		t.Fatalf("Ctrl+Enter must not edit the text: %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := c.Text(); got == before {
		t.Fatal("plain Enter must still insert a newline")
	}
}

func TestCodeEditorKeepsUndoAcrossConsoleSwitch(t *testing.T) {
	c := newTestCode()
	c.SetActive("a", "SELECT 1")
	c.Insert("x")
	edited := c.Text()
	if !c.state.Buffer.CanUndo() {
		t.Fatal("typing must create an undo step")
	}
	c.SetActive("b", "SELECT 2")
	c.SetActive("a", edited) // 切回来时正文与 Buffer 一致：不得清空撤销历史
	if !c.state.Buffer.CanUndo() {
		t.Fatal("undo history must survive switching between consoles")
	}
	c.state.Buffer.Undo()
	if got := c.Text(); got != "SELECT 1" {
		t.Fatalf("undo after switch: %q", got)
	}
}

func TestFormatActiveFormatsDocument(t *testing.T) {
	a := newConsoleRoot(t)
	a.setActiveText("select a,b from t where x=1")
	a.setSelection(0, 0)
	a.formatActive()
	want := "SELECT a,b\nFROM t\nWHERE x=1"
	if got := a.activeText(); got != want {
		t.Fatalf("format:\n%q\nwant\n%q", got, want)
	}
	if !a.code.state.Buffer.CanUndo() {
		t.Fatal("formatting must be undoable")
	}
}

func TestFormatActiveFormatsSelectionOnly(t *testing.T) {
	a := newConsoleRoot(t)
	a.setActiveText("-- keep me as is\nselect 1")
	start := len([]rune("-- keep me as is\n"))
	a.setSelection(start, start+len([]rune("select 1")))
	a.formatActive()
	if got := a.activeText(); got != "-- keep me as is\nSELECT 1" {
		t.Fatalf("selection format: %q", got)
	}
}

// 3 位数行号时，状态圆点必须留在行号数字左侧：圆点右缘不超过编辑器
// 左起 8 DIP。编辑器的 GutterMarks 从 +3 画 9 DIP 的点，会压到行号。
func TestGutterDotsStayLeftOfLineNumbers(t *testing.T) {
	c := newTestCode()
	lines := make([]string, 0, 120)
	for i := 1; i <= 120; i++ {
		lines = append(lines, fmt.Sprintf("SELECT %d;", i))
	}
	c.SetActive("a", strings.Join(lines, "\n"))
	c.state.ScrollTo(100)
	var th *ui.Theme
	tt := ui.NewTester(func(ctx *ui.Context) {
		th = ctx.Theme()
		c.marks = []myeditor.GutterMark{{Line: 100, Color: th.Success}}
		c.ViewWithDots(ctx).Label("ed")
	}, 800, 400)
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find("ed")
	if !ok {
		t.Fatal("编辑器缺少标签")
	}
	img := tt.Image()
	maxX := -1
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			if p.R == th.Success.R && p.G == th.Success.G && p.B == th.Success.B {
				maxX = max(maxX, x)
			}
		}
	}
	if maxX < 0 {
		t.Fatal("没有画出绿色状态圆点")
	}
	if got := float32(maxX) - r.X; got > 8 {
		t.Fatalf("状态圆点压到行号：右缘 x=%v > 8", got)
	}
}
