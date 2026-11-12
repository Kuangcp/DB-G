package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"db-g/engine"
)

// TestAppIconPixels 验证图标真的画出来了：浅色圆角底 + 紫→蓝渐变的描边 G，中心镂空。
func TestAppIconPixels(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(appIconPNG))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("size: %v", b)
	}
	at := func(x, y int) (uint8, uint8, uint8, uint8) {
		r, g, bl, a := img.At(x, y).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)
	}
	// 圆角外透明，上边缘内侧是浅色底（#FBFBFF）。
	if _, _, _, a := at(0, 0); a > 60 {
		t.Fatalf("corner must be transparent, got a=%d", a)
	}
	if r, g, bl, a := at(128, 20); a < 200 || r < 245 || g < 245 || bl < 250 {
		t.Fatalf("background must be light, got %d,%d,%d a=%d", r, g, bl, a)
	}
	// 渐变方向：上环偏紫、下环偏蓝青、横杠偏蓝，且不是纯色。
	tr, tg, tb, _ := at(128, 68)
	br, bg2, bb, _ := at(128, 188)
	mr, mg, mb, _ := at(170, 137)
	if tr < 90 || tr > 140 || tg > 130 || tb < 245 {
		t.Fatalf("top of G must be purple, got %d,%d,%d", tr, tg, tb)
	}
	if br > 110 || bg2 < 125 || bg2 > 185 || bb < 240 {
		t.Fatalf("bottom of G must be blue-cyan, got %d,%d,%d", br, bg2, bb)
	}
	if mr < 55 || mr > 120 || mg < 115 || mg > 180 || mb < 240 {
		t.Fatalf("G bar must be blue, got %d,%d,%d", mr, mg, mb)
	}
	if tr == br && tg == bg2 && tb == bb {
		t.Fatal("G must be a gradient, not a flat color")
	}
	// 环内是浅色底
	if r, g, bl, _ := at(128, 100); r < 245 || g < 245 || bl < 250 {
		t.Fatalf("hole must be light, got %d,%d,%d", r, g, bl)
	}
	if _, err := os.Stat("assets/icon.svg"); err != nil {
		t.Fatalf("svg source missing: %v", err)
	}
}

// TestAllConnKindsLinked 保证 main 把每个数据源的 driver 都链接进来了。少一个
// `_ "db-g/driver/xxx"`，它的 init() 就不跑，注册表里没有这个类型，新建连接时
// 一打开就报 "engine: unknown data source kind"。
func TestAllConnKindsLinked(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres", "clickhouse"} {
		if !engine.Registered(kind) {
			t.Fatalf("%s 未注册：main.go 缺 _ \"db-g/driver/%s\"", kind, kind)
		}
	}
}

func TestOpenPathsFromSecondInstance(t *testing.T) {
	wd := t.TempDir()
	abs := filepath.Join(t.TempDir(), "b.sql")
	got := openPathsFromArgs([]string{"-o", "a.sql", "-o=" + abs}, wd)
	want := []string{filepath.Join(wd, "a.sql"), abs}
	if !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if got := openPathsFromArgs(nil, wd); len(got) != 0 {
		t.Fatalf("no -o must open nothing: %v", got)
	}
}
