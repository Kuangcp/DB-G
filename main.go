package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type app struct {
	ed *editor
}

func newApp(text string, s strats) *app {
	return &app{ed: NewEditor(text, s)}
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}

func rootView(a *app) func(c *ui.Context) {
	return func(c *ui.Context) {
		ed := a.ed
		ed.Update()

		ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
			ui.Text(c, fmt.Sprintf(
				"lines=%d ranges=%d tokenize=%s inject=%s build=%s",
				ed.lineCount(), ed.Injected,
				scopeName(ed.strat.tokenize), scopeName(ed.strat.inject),
				fmtDur(appLastBuild),
			)).FontSize(12)

			ui.Row(c).Fill().Children(func() {
				ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
					ed.viewH = r.H
					p.Clip(r, 0, func() {
						top := r.Y - ed.scroll.Y
						for i := ed.lines.At(ed.scroll.Y); i < ed.lines.Count(); i++ {
							y := top + ed.lines.Top(i)
							if y > r.Y+r.H {
								break
							}
							p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
						}
					})
				})

				in := ui.TextAreaBuffer(c, ed.Buf).Label("编辑器").Fill().Font("monospace").
					TrackLines(&ed.lines).TrackScroll(&ed.scroll)
				in.TextRanges(ed.allRanges...)
			})
		})
	}
}

func main() {
	sc := strats{scopeViewport, scopeViewport}
	switch os.Getenv("SCOPE") {
	case "FF":
		sc = strats{scopeFull, scopeFull}
	case "FV":
		sc = strats{scopeFull, scopeViewport}
	}
	a := newApp(buildSQL(3_000), sc)

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-k mygo spike",
			Width:   900,
			Height:  640,
			Content: ui.View(rootView(a)),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}