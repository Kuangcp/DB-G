package main

import (
	"bytes"
	"image/png"
	"os"
	"testing"

	"db-g/engine"
)

// TestAppIconPixels 验证自绘图标真的画出来了：品牌蓝底 + 白色 G（环与横杠），中心是镂空。
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
	// 圆角外是透明的，取上边缘内侧验证白色底。
	r, g, bl, a := at(128, 20)
	if a < 200 || r < 240 || g < 240 || bl < 240 {
		t.Fatalf("background must be white, got %d,%d,%d a=%d", r, g, bl, a)
	}
	// 字母 G 用品牌渐变：上环偏蓝、下环偏靛，二者不同且都不是白色。
	tr, tg, tb, _ := at(128, 70)
	br, bg2, bb, _ := at(128, 186)
	if tr > 140 || tb < 170 {
		t.Fatalf("top of G must be blue-ish, got %d,%d,%d", tr, tg, tb)
	}
	if br > 160 || bb < 200 {
		t.Fatalf("bottom of G must be indigo-ish, got %d,%d,%d", br, bg2, bb)
	}
	if tr == br && tg == bg2 && tb == bb {
		t.Fatal("G must be a gradient, not a flat color")
	}
	// 环内是白底
	if r, g, bl, _ := at(128, 100); r < 240 || g < 240 || bl < 240 {
		t.Fatalf("hole must be white, got %d,%d,%d", r, g, bl)
	}
	// 横杠是渐变颜色（非白）
	if r, g, bl, _ := at(170, 128); r > 220 && g > 220 && bl > 220 {
		t.Fatalf("G bar must be colored, got %d,%d,%d", r, g, bl)
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
