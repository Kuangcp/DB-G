// Command genicon 生成 db-g 的应用图标（自绘「G」标）：输出 assets/icon-{256,512}.png。
//
// 设计对齐 db-k 母版：**白色圆角底 + 细边**（深色任务栏下对比度最高），字母用品牌渐变
// （蓝 #2F6FE4 → 青 #22B8E6 → 靛 #6C63FF，左上到右下）。不依赖 SVG 渲染器，用
// x/image/vector 直接光栅化，任何构建机都能重现。
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
	colWhite  = 0xFFFFFF
	colBorder = 0xC8D6EC
	colGradA  = 0x2F6FE4 // 品牌蓝
	colGradB  = 0x22B8E6 // 青
	colGradC  = 0x6C63FF // 靛
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
	radius := s * 0.20

	// 白色圆角底 + 细边（先画边色整块，再内缩画白色，留出 1.5% 的描边）。
	border := newRaster(size)
	roundedRect(border, 0, 0, s, s, radius)
	fill(img, border, hexColor(colBorder))
	pad := s * 0.012
	base := newRaster(size)
	roundedRect(base, pad, pad, s-pad, s-pad, radius-pad)
	fill(img, base, hexColor(colWhite))

	// 字母 G 的遮罩：环 - 缺口 + 横杠。
	mask := image.NewAlpha(img.Bounds())
	cx, cy := s/2, s/2
	outer, inner := s*0.30, s*0.185
	ring := newRaster(size)
	circle(ring, cx, cy, outer, false)
	circle(ring, cx, cy, inner, true)
	zapSrc(mask, ring)

	// 缺口：先求出缺口覆盖，再从字母遮罩里减掉（Src 会清掉路径外像素，不能直接用）。
	cut := image.NewAlpha(img.Bounds())
	gap := newRaster(size)
	rectPath(gap, cx+inner*0.55, cy-s*0.16, cx+outer*1.15, cy+s*0.16)
	zapSrc(cut, gap)
	for i := range mask.Pix {
		mask.Pix[i] = uint8(uint32(mask.Pix[i]) * (0xFFFF - uint32(cut.Pix[i])*0x101) / 0xFFFF)
	}

	bar := newRaster(size)
	rectPath(bar, cx-inner*0.15, cy-s*0.055, cx+outer*0.86, cy+s*0.055)
	zapOver(mask, bar)

	// 渐变上色：沿字母包围盒对角线，三段色线性插值。
	grad := linearGradient(colGradA, colGradB, colGradC)
	x0, y0, x1, y1 := cx-outer, cy-outer, cx+outer, cy+outer
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := mask.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			t := ((float64(x)-x0)/(x1-x0) + (float64(y)-y0)/(y1-y0)) / 2
			c := grad(t)
			blendOver(img, x, y, c, uint32(a)*0x101)
		}
	}

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

// zapSrc 用 Src 覆盖遮罩（要求遮罩原本为空，否则路径外像素也会被清零）。
func zapSrc(mask *image.Alpha, z *vector.Rasterizer) {
	z.DrawOp = draw.Src
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
}

// zapOver 把形状以 Over 画到遮罩上（路径外像素保持不变）。
func zapOver(mask *image.Alpha, z *vector.Rasterizer) {
	z.DrawOp = draw.Over
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
}

// linearGradient 返回 t∈[0,1] 的三段线性渐变（A→B→C）。
func linearGradient(a, b, c uint32) func(t float64) color.RGBA {
	return func(t float64) color.RGBA {
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		if t < 0.5 {
			return lerpColor(a, b, t*2)
		}
		return lerpColor(b, c, (t-0.5)*2)
	}
}

func lerpColor(a, b uint32, t float64) color.RGBA {
	ar, ag, ab := float64(a>>16&0xFF), float64(a>>8&0xFF), float64(a&0xFF)
	br, bg, bb := float64(b>>16&0xFF), float64(b>>8&0xFF), float64(b&0xFF)
	return color.RGBA{
		R: uint8(ar + (br-ar)*t),
		G: uint8(ag + (bg-ag)*t),
		B: uint8(ab + (bb-ab)*t),
		A: 255,
	}
}

// blendOver 把颜色 c 以 alpha（0..0xFFFF）叠到像素上。
func blendOver(img *image.RGBA, x, y int, c color.RGBA, alpha uint32) {
	i := img.PixOffset(x, y)
	a := alpha
	inv := 0xFFFF - a
	img.Pix[i+0] = uint8((uint32(img.Pix[i+0])*inv + uint32(c.R)*a) / 0xFFFF)
	img.Pix[i+1] = uint8((uint32(img.Pix[i+1])*inv + uint32(c.G)*a) / 0xFFFF)
	img.Pix[i+2] = uint8((uint32(img.Pix[i+2])*inv + uint32(c.B)*a) / 0xFFFF)
	img.Pix[i+3] = 255
}

func rectPath(z *vector.Rasterizer, x0, y0, x1, y1 float64) {
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
