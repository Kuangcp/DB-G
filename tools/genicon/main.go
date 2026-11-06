// Command genicon 生成 db-g 的应用图标（自绘「G」标）：输出 assets/icon-{256,512}.png。
//
// 设计：圆角方底（品牌蓝）+ 白色 G（环 + 右侧缺口 + 横杠）。不依赖 SVG 渲染器，
// 用 x/image/vector 直接光栅化，保证任何构建机都能重现。
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

const (
	bgBrand = 0x2D6CDF // 品牌蓝
	fgWhite = 0xFFFFFF
)

func main() {
	out := "assets"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, size := range []int{512, 256} {
		if err := writeIcon(filepath.Join(out, "icon-"+itoa(size)+".png"), size); err != nil {
			log.Fatalf("icon %d: %v", size, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// writeIcon 画一张 size×size 的图标。
func writeIcon(path string, size int) error {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)

	// 背景：圆角方（半径 s*0.20）
	bgColor := hexColor(bgBrand)
	bg := newRaster(size)
	roundedRect(bg, 0, 0, s, s, s*0.20)
	fill(img, bg, bgColor)

	// G 的环：外圆 - 内圆（反向绕，镂空）
	white := hexColor(fgWhite)
	cx, cy := s/2, s/2
	outer, inner := s*0.30, s*0.185
	ring := newRaster(size)
	circle(ring, cx, cy, outer, false)
	circle(ring, cx, cy, inner, true)
	fill(img, ring, white)

	// 缺口：右侧 (-28°..28°) 用背景色盖掉
	gap := newRaster(size)
	gx0, gx1 := cx+inner*0.55, cx+outer*1.15
	gy0, gy1 := cy-s*0.16, cy+s*0.16
	rect(gap, gx0, gy0, gx1, gy1)
	fill(img, gap, bgColor)

	// 横杠：从中心到右侧
	bar := newRaster(size)
	rect(bar, cx-inner*0.15, cy-s*0.055, cx+outer*0.86, cy+s*0.055)
	fill(img, bar, white)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// newRaster 建一个尺寸已知的光栅器（必须先 Reset，否则 Draw 会 panic）。
func newRaster(size int) *vector.Rasterizer {
	z := &vector.Rasterizer{}
	z.Reset(size, size)
	return z
}

func fill(dst draw.Image, z *vector.Rasterizer, c color.RGBA) {
	z.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

func rect(z *vector.Rasterizer, x0, y0, x1, y1 float64) {
	z.MoveTo(float32(x0), float32(y0))
	z.LineTo(float32(x1), float32(y0))
	z.LineTo(float32(x1), float32(y1))
	z.LineTo(float32(x0), float32(y1))
	z.ClosePath()
}

// roundedRect 画圆角矩形（四角三次贝塞尔）。
func roundedRect(z *vector.Rasterizer, x0, y0, x1, y1, r float64) {
	k := 0.5522847498 * r
	z.MoveTo(float32(x0+r), float32(y0))
	z.LineTo(float32(x1-r), float32(y0))
	z.CubeTo(float32(x1-r+k), float32(y0), float32(x1), float32(y0+r-k), float32(x1), float32(y0+r))
	z.LineTo(float32(x1), float32(y1-r))
	z.CubeTo(float32(x1), float32(y1-r+k), float32(x1-r+k), float32(y1), float32(x1-r), float32(y1))
	z.LineTo(float32(x0+r), float32(y1))
	z.CubeTo(float32(x0+r-k), float32(y1), float32(x0), float32(y1-r+k), float32(x0), float32(y1-r))
	z.LineTo(float32(x0), float32(y0+r))
	z.CubeTo(float32(x0), float32(y0+r-k), float32(x0+r-k), float32(y0), float32(x0+r), float32(y0))
	z.ClosePath()
}

// circle 画圆；ccw=true 时反向（用于镂空）。
func circle(z *vector.Rasterizer, cx, cy, r float64, ccw bool) {
	k := 0.5522847498 * r
	if !ccw {
		z.MoveTo(float32(cx+r), float32(cy))
		z.CubeTo(float32(cx+r), float32(cy+k), float32(cx+k), float32(cy+r), float32(cx), float32(cy+r))
		z.CubeTo(float32(cx-k), float32(cy+r), float32(cx-r), float32(cy+k), float32(cx-r), float32(cy))
		z.CubeTo(float32(cx-r), float32(cy-k), float32(cx-k), float32(cy-r), float32(cx), float32(cy-r))
		z.CubeTo(float32(cx+k), float32(cy-r), float32(cx+r), float32(cy-k), float32(cx+r), float32(cy))
		z.ClosePath()
		return
	}
	z.MoveTo(float32(cx+r), float32(cy))
	z.CubeTo(float32(cx+r), float32(cy-k), float32(cx+k), float32(cy-r), float32(cx), float32(cy-r))
	z.CubeTo(float32(cx-k), float32(cy-r), float32(cx-r), float32(cy-k), float32(cx-r), float32(cy))
	z.CubeTo(float32(cx-r), float32(cy+k), float32(cx-k), float32(cy+r), float32(cx), float32(cy+r))
	z.CubeTo(float32(cx+k), float32(cy+r), float32(cx+r), float32(cy+k), float32(cx+r), float32(cy))
	z.ClosePath()
}

// hexColor 把 0xRRGGBB 常量转成颜色（先转 uint32 变量，避免未类型化常量溢出 byte）。
func hexColor(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}
