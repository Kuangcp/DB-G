package ui

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

type root struct {
	ed            *editor
	findQuery     string
	findActive    bool
	sampler       *Sampler
	con           *app.Console
	win           *mygo.Window
	resultState   ui.ListState
	resultsHeight float32
}

func newRoot(text string, s strats) *root {
	return &root{
		ed:            NewEditor(text, s),
		sampler:       NewSampler(128),
		con:           app.NewConsole(),
		resultsHeight: 260,
	}
}

// NewRoot 创建交互窗口状态（初始 3k 行 SQL，SCOPE 环境变量选策略）。
func NewRoot() *root {
	return newRoot(buildSQL(3_000), stratsFromEnv())
}

func stratsFromEnv() strats {
	switch os.Getenv("SCOPE") {
	case "FF":
		return strats{scopeFull, scopeFull}
	case "FV":
		return strats{scopeFull, scopeViewport}
	}
	return strats{scopeViewport, scopeViewport}
}

// Attach 在窗口创建后接通主线程回投与原生窗口句柄。
func (a *root) Attach(win *mygo.Window) {
	a.win = win
	a.con.SetPublisher(func(f func()) { win.Update(f) })
}

// View 是窗口内容入口。
func (a *root) View(c *ui.Context) {
	rootView(a)(c)
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}

func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		ed := a.ed
		ed.Update()

		ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
			// HUD
			ui.Text(c, fmt.Sprintf(
				"lines=%d injected=%d tokenize=%s inject=%s build(last)=%s",
				ed.lineCount(), ed.Injected,
				scopeName(ed.strat.tokenize), scopeName(ed.strat.inject),
				fmtDur(appLastBuild),
			)).FontSize(12)

			// toolbar: sizes + strategies
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "3k").Clicked() {
					ed.Buf.Set(buildSQL(3_000))
					ed.dirty = true
				}
				if ui.Button(c, "12k").Clicked() {
					ed.Buf.Set(buildSQL(12_000))
					ed.dirty = true
				}
				if ui.Button(c, "100k").Clicked() {
					ed.Buf.Set(buildSQL(100_000))
					ed.dirty = true
				}
				ui.Spacer(c)
				if ui.Button(c, "FF").Clicked() {
					ed.SetStrategy(strats{scopeFull, scopeFull})
				}
				if ui.Button(c, "FV").Clicked() {
					ed.SetStrategy(strats{scopeFull, scopeViewport})
				}
				if ui.Button(c, "VV").Clicked() {
					ed.SetStrategy(strats{scopeViewport, scopeViewport})
				}
			})

			// toolbar: find
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &a.findQuery).Placeholder("find (overrides highlight when active)")
				if ui.Button(c, "Find").Clicked() {
					a.findActive = true
				}
				if ui.Button(c, "Clear").Clicked() {
					a.findQuery, a.findActive = "", false
				}
			})

			// 编辑器 / 结果 分屏
			ui.SplitVertical(c, &a.resultsHeight, func() {
				ui.Row(c).Fill().Children(func() {
					ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
						ed.viewH = r.H
						p.Clip(r, 0, func() {
							top := r.Y - ed.scroll.Y
							cur := ed.lines.At(ed.scroll.Y)
							for i := cur; i < ed.lines.Count(); i++ {
								y := top + ed.lines.Top(i)
								if y > r.Y+r.H {
									break
								}
								if i == cur {
									p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 16}, ui.RGBA(80, 100, 160, 0.25), 0)
								}
								p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
							}
						})
					})

					in := ui.TextAreaBuffer(c, ed.Buf).Label("编辑器").Fill().Font("monospace").
						TrackLines(&ed.lines).TrackScroll(&ed.scroll)
					if a.findActive && a.findQuery != "" {
						in.TextRanges(FindAll(ed.Buf.String(), a.findQuery)...)
					} else {
						in.TextRanges(ed.allRanges...)
					}
				})
			}, func() {
				a.resultPane(c)
			}).Grow(1)
		})
	}
}
