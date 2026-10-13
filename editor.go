package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"db-k/highlight"
)

// appLastBuild reports how long the last editor rebuild (copy + tokenize +
// ranges) took; read by the HUD. Set in rebuild.
var appLastBuild time.Duration

type editorScope int

const (
	scopeViewport editorScope = iota
	scopeFull
)

func scopeName(s editorScope) string {
	if s == scopeFull {
		return "full"
	}
	return "viewport"
}

type strats struct {
	tokenize editorScope
	inject   editorScope
}

type editor struct {
	Buf   *ui.TextBuffer
	strat strats
	// margin is extra lines tokenized/injected beyond the viewport.
	margin int

	lines  ui.TextLines
	scroll ui.ScrollState
	viewH  float32

	allRanges []ui.TextRange
	Injected  int
	dirty     bool

	lastVersion    uint64
	byteStarts     []int
	runeStarts     []int
	lastLo, lastHi int
}

func NewEditor(text string, s strats) *editor {
	return &editor{Buf: ui.NewTextBuffer(text), strat: s, margin: 4}
}

func (ed *editor) SetStrategy(s strats) {
	if ed.strat != s {
		ed.strat = s
		ed.dirty = true
	}
}

func (ed *editor) lineCount() int {
	if n := ed.Buf.LineCount(); n > 0 {
		return n
	}
	return ed.lines.Count()
}

// window returns the line window [lo,hi) to tokenize/inject for the
// viewport strategy, from tracked scroll + gutter height + margin.
func (ed *editor) window() (int, int) {
	n := ed.lineCount()
	if n == 0 {
		return 0, 0
	}
	lo := ed.lines.At(ed.scroll.Y)
	hi := ed.lines.At(ed.scroll.Y+ed.viewH) + 1
	if hi < lo {
		hi = lo + 1
	}
	lo -= ed.margin
	if lo < 0 {
		lo = 0
	}
	hi += ed.margin
	if hi > n {
		hi = n
	}
	if lo >= hi {
		return lo, min(lo+1, n)
	}
	return lo, hi
}

// Update rebuilds ed.allRanges when the text version changed, the viewport
// window moved (viewport strategy), or a strategy switch was requested.
func (ed *editor) Update() {
	ver := ed.Buf.Version()
	lo, hi := 0, ed.lineCount()
	if ed.strat.inject == scopeViewport || ed.strat.tokenize == scopeViewport {
		lo, hi = ed.window()
	}
	if ver == ed.lastVersion && !ed.dirty && ed.lastLo == lo && ed.lastHi == hi {
		return
	}
	ed.rebuild(lo, hi)
	ed.lastVersion, ed.lastLo, ed.lastHi = ver, lo, hi
	ed.dirty = false
}

func (ed *editor) rebuild(lo, hi int) {
	t0 := time.Now()
	txt := ed.Buf.String()
	ed.byteStarts, ed.runeStarts = highlight.LineStarts(txt)
	n := len(ed.byteStarts)
	if hi > n {
		hi = n
	}
	if lo > n {
		lo = n
	}

	tokenLo, tokenHi := 0, n
	if ed.strat.tokenize == scopeViewport {
		tokenLo, tokenHi = lo, hi
	}
	injLo, injHi := 0, n
	if ed.strat.inject == scopeViewport {
		injLo, injHi = lo, hi
	}
	if injLo < tokenLo || injHi > tokenHi {
		tokenLo, tokenHi = injLo, injHi // tokenize must cover inject
	}

	runs := highlight.Tokenize(txt, ed.byteStarts, tokenLo, tokenHi)
	ed.allRanges = highlight.Ranges(runs, ed.runeStarts, injLo, injHi)
	ed.Injected = len(ed.allRanges)
	appLastBuild = time.Since(t0)
}