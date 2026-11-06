package main

import (
	"bytes"
	"image/png"
	"os"
	"testing"
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
	// 圆角外是透明的，取上边缘内侧验证品牌蓝底。
	r, g, bl, a := at(128, 20)
	if a < 200 || r > 80 || g < 90 || bl < 180 {
		t.Fatalf("background must be brand blue, got %d,%d,%d a=%d", r, g, bl, a)
	}
	if r, g, bl, _ := at(128, 70); r < 240 || g < 240 || bl < 240 {
		t.Fatalf("ring must be white, got %d,%d,%d", r, g, bl)
	}
	if r, g, bl, _ := at(128, 100); bl < 180 || r > 80 {
		t.Fatalf("G must have a hole above the bar, got %d,%d,%d", r, g, bl)
	}
	if r, g, bl, _ := at(170, 128); r < 240 || g < 240 || bl < 240 {
		t.Fatalf("G bar must be white, got %d,%d,%d", r, g, bl)
	}
	if _, err := os.Stat("assets/icon.svg"); err != nil {
		t.Fatalf("svg source missing: %v", err)
	}
}
