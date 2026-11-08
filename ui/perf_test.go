package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"
)

// perfSizes 是基准的文档规模（行）。
var perfSizes = []int{3_000, 12_000, 100_000}

// perfLimits 是各场景的 p99 上限；由实测 p99 × 1.5 得出（见 docs/spike-results.md）。
type perfLimits struct {
	typing, scroll, firstFrame, completion time.Duration
}

var perfBudget = map[int]perfLimits{
	// 首帧：3k/12k 实测 ~0.84ms（限 5ms）；100k 是「开盘预热 3 帧 ≈ 228–255ms」的一次性成本，
	// 依据用户拍板接受（见 docs/spike-results.md 的 A1 一节）。
	3_000:   {typing: 12 * time.Millisecond, scroll: 2 * time.Millisecond, firstFrame: 5 * time.Millisecond, completion: 14 * time.Millisecond},
	12_000:  {typing: 12 * time.Millisecond, scroll: 2 * time.Millisecond, firstFrame: 5 * time.Millisecond, completion: 14 * time.Millisecond},
	100_000: {typing: 12 * time.Millisecond, scroll: 2 * time.Millisecond, firstFrame: 300 * time.Millisecond, completion: 15 * time.Millisecond},
}

// steadyFrameLimit 是「预热之后」的空帧上限：真正决定交互手感的那一档。
const steadyFrameLimit = 2 * time.Millisecond

// newPerfEditor 返回一个已聚焦、光标在文末的编辑器测试床。
func newPerfEditor(t *testing.T, lines int, completionItems int) (*codeEditor, *ui.Tester) {
	t.Helper()
	c := newCodeEditor("")
	c.SetActive("perf", buildSQL(lines))
	if completionItems > 0 {
		items := make([]myeditor.Completion, completionItems)
		for i := range items {
			items[i] = myeditor.Completion{Label: "keyword_" + string(rune('a'+i%26)), Kind: myeditor.CompletionKeyword}
		}
		c.state.OnCompletion = func(r myeditor.CompletionRequest) {
			c.state.ShowCompletions(r.Offset-len(r.Prefix), items)
		}
	}
	total := len([]rune(c.Text()))
	c.SetSelection(total, total)
	tt := ui.NewTester(func(ctx *ui.Context) { c.View(ctx).AutoFocus() }, 1200, 800)
	tt.Frame()
	return c, tt
}

func sampleFrames(n int, step func()) []time.Duration {
	out := make([]time.Duration, 0, n)
	for range n {
		start := time.Now()
		step()
		out = append(out, time.Since(start))
	}
	return out
}

func skipPerf(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("性能基准在 CI 的 -short 模式跳过")
	}
}

func TestEditorTypingPerf(t *testing.T) {
	skipPerf(t)
	for _, size := range perfSizes {
		c, tt := newPerfEditor(t, size, 0)
		_ = c
		samples := sampleFrames(20, func() {
			tt.TypeKey(0, ui.KeyA, "a")
			tt.Frame()
		})
		p50, p99 := median(samples), pct(samples, 0.99)
		t.Logf("size=%d typing: p50=%v p99=%v", size, p50, p99)
		if lim, ok := perfBudget[size]; ok && lim.typing > 0 && p99 > lim.typing {
			t.Fatalf("size=%d typing p99 %v exceeds %v", size, p99, lim.typing)
		}
	}
}

func TestEditorScrollPerf(t *testing.T) {
	skipPerf(t)
	for _, size := range perfSizes {
		_, tt := newPerfEditor(t, size, 0)
		samples := sampleFrames(20, func() {
			tt.Scroll(600, 400, 0, 400)
			tt.Frame()
		})
		p50, p99 := median(samples), pct(samples, 0.99)
		t.Logf("size=%d scroll: p50=%v p99=%v", size, p50, p99)
		if lim, ok := perfBudget[size]; ok && lim.scroll > 0 && p99 > lim.scroll {
			t.Fatalf("size=%d scroll p99 %v exceeds %v", size, p99, lim.scroll)
		}
	}
}

func TestEditorFirstFramePerf(t *testing.T) {
	skipPerf(t)
	for _, size := range perfSizes {
		c := newCodeEditor("")
		c.SetActive("perf", buildSQL(size))
		total := len([]rune(c.Text()))
		c.SetSelection(total, total)
		tt := ui.NewTester(func(ctx *ui.Context) { c.View(ctx).AutoFocus() }, 1200, 800)
		start := time.Now()
		tt.Frame()
		d := time.Since(start)
		t.Logf("size=%d first frame: %v", size, d)
		if lim, ok := perfBudget[size]; ok && lim.firstFrame > 0 && d > lim.firstFrame {
			t.Fatalf("size=%d first frame %v exceeds %v", size, d, lim.firstFrame)
		}
	}
}

func TestEditorTypingWithCompletionsOpen(t *testing.T) {
	skipPerf(t)
	for _, size := range perfSizes {
		_, tt := newPerfEditor(t, size, 2_000)
		samples := sampleFrames(20, func() {
			tt.TypeKey(0, ui.KeyA, "a")
			tt.Frame()
		})
		p50, p99 := median(samples), pct(samples, 0.99)
		t.Logf("size=%d completion-open typing: p50=%v p99=%v", size, p50, p99)
		if lim, ok := perfBudget[size]; ok && lim.completion > 0 && p99 > lim.completion {
			t.Fatalf("size=%d completion typing p99 %v exceeds %v", size, p99, lim.completion)
		}
	}
}

// TestEditorSteadyFramePerf 测预热后的空帧：首帧的一次性成本不算进手感预算。
func TestEditorSteadyFramePerf(t *testing.T) {
	skipPerf(t)
	for _, size := range perfSizes {
		_, tt := newPerfEditor(t, size, 0)
		for range 5 { // 预热：消化一次性的行索引/高亮状态
			tt.Frame()
		}
		samples := sampleFrames(20, func() { tt.Frame() })
		p50, p99 := median(samples), pct(samples, 0.99)
		t.Logf("size=%d steady frame: p50=%v p99=%v", size, p50, p99)
		if p99 > steadyFrameLimit {
			t.Fatalf("size=%d steady frame p99 %v exceeds %v", size, p99, steadyFrameLimit)
		}
	}
}
