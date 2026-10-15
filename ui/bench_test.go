package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type cfgKey struct {
	name   string
	tok    editorScope
	inject editorScope
}

var matrixCfg = []cfgKey{
	{"FF", scopeFull, scopeFull},
	{"FV", scopeFull, scopeViewport},
	{"VV", scopeViewport, scopeViewport},
}

func median(acc []time.Duration) time.Duration {
	if len(acc) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), acc...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func pct(acc []time.Duration, p float64) time.Duration {
	if len(acc) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), acc...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(p * float64(len(s)))
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

func measureOne(size int, ck cfgKey) (injected int, keys, scrolls, frames []time.Duration) {
	a := newRoot(buildSQL(size), strats{tokenize: ck.tok, inject: ck.inject})
	tt := ui.NewTester(rootView(a), 900, 640)
	for range 4 {
		tt.Frame()
	}
	injected = a.ed.Injected

	if err := tt.Click("编辑器"); err != nil {
		logSpike("size=%d cfg=%s focus editor: %v", size, ck.name, err)
	}
	for range 24 {
		t0 := time.Now()
		tt.Type("a")
		keys = append(keys, time.Since(t0))
	}
	for range 12 {
		t0 := time.Now()
		tt.Scroll(0, 0, 0, 40)
		scrolls = append(scrolls, time.Since(t0))
	}
	for range 12 {
		t0 := time.Now()
		tt.Frame()
		frames = append(frames, time.Since(t0))
	}
	return injected, keys, scrolls, frames
}

func logSpike(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "spike: "+format+"\n", args...)
}

func TestSpikeMatrix(t *testing.T) {
	sizes := []int{3_000, 12_000, 100_000}
	dir := "/tmp/opencode"
	_ = os.MkdirAll(dir, 0o755)
	for _, size := range sizes {
		for _, ck := range matrixCfg {
			injected, keys, scrolls, frames := measureOne(size, ck)
			t.Logf("size=%-6d cfg=%-3s injected=%-7d "+
				"key med=%-8v p90=%-8v p99=%-8v | "+
				"scroll med=%-8v p90=%-8v | "+
				"frame med=%-8v",
				size, ck.name, injected,
				median(keys), pct(keys, 0.9), pct(keys, 0.99),
				median(scrolls), pct(scrolls, 0.9),
				median(frames))

			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("matrix-%d-%s.csv", size, ck.name)))
			if err != nil {
				t.Logf("csv dump skipped: %v", err)
				continue
			}
			for _, d := range keys {
				fmt.Fprintf(f, "key,%d\n", d.Nanoseconds())
			}
			for _, d := range scrolls {
				fmt.Fprintf(f, "scroll,%d\n", d.Nanoseconds())
			}
			for _, d := range frames {
				fmt.Fprintf(f, "frame,%d\n", d.Nanoseconds())
			}
			f.Close()
		}
	}
}