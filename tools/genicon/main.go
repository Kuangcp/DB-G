// Command genicon 生成 db-g 的应用图标：输出 assets/icon-{256,512}.png。
//
// 几何与配色与 assets/icon.svg 一致：浅色圆角底 + 2px 细边，字母 G 为 52px
// 等宽描边（右上开口、横杠下移），渐变从 #7C4DFF 流到 #38BDF8。位图用
// x/image/vector 直接光栅化，不依赖外部 SVG 渲染器，任何构建机都能重现。
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

const (
	viewBox   = 512.0
	tileR     = 112.0
	borderW   = 2.0
	iconBg    = 0xFBFBFF
	iconEdge  = 0xE9E7FA
	gCx       = 256.0
	gCy       = 256.0
	gRadius   = 120.0
	gStroke   = 52.0
	gStartDeg = -38.0
	gEndDeg   = 9.0
	gBarX     = 300.0
)

// 渐变与 icon.svg 的 objectBoundingBox 对角线一致；bbox 取路径几何范围。
var (
	gradX0 = gCx - gRadius                        // 136
	gradY0 = gCy - gRadius                        // 136
	gradX1 = gCx + gRadius*math.Cos(deg(gEndDeg)) // 374.52
	gradY1 = gCy + gRadius                        // 376（圆弧经过最低点）
)

var iconStops = []struct {
	at  float64
	rgb uint32
}{
	{0.00, 0x7C4DFF},
	{0.55, 0x6A7BFF},
	{1.00, 0x38BDF8},
}

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
	k := float64(size) / viewBox

	// 圆角底：先整块描边色，再向内缩 2px 画底色，圆角外保持透明。
	edge := newRaster(size)
	roundedRect(edge, 0, 0, viewBox*k, viewBox*k, tileR*k)
	fill(img, edge, hexColor(iconEdge))
	base := newRaster(size)
	roundedRect(base, borderW*k, borderW*k, (viewBox-borderW)*k, (viewBox-borderW)*k, (tileR-borderW)*k)
	fill(img, base, hexColor(iconBg))

	// G 字形的描边轮廓：圆环带 + 起点圆头 + 横杠胶囊（胶囊右端同时补出圆角接头）。
	mask := image.NewAlpha(img.Bounds())
	band := newRaster(size)
	annulus(band, gCx*k, gCy*k, gRadius*k, gStroke*k, gStartDeg, gEndDeg)
	zapOver(mask, band)

	startX := gCx + gRadius*math.Cos(deg(gStartDeg))
	startY := gCy + gRadius*math.Sin(deg(gStartDeg))
	capR := gStroke / 2 * k
	cap := newRaster(size)
	circle(cap, startX*k, startY*k, capR, false)
	zapOver(mask, cap)

	endX := gCx + gRadius*math.Cos(deg(gEndDeg))
	endY := gCy + gRadius*math.Sin(deg(gEndDeg))
	bar := newRaster(size)
	roundedRect(bar, (gBarX-gStroke/2)*k, (endY-gStroke/2)*k, (endX+gStroke/2)*k, (endY+gStroke/2)*k, gStroke/2*k)
	zapOver(mask, bar)

	// 沿 bbox 对角线铺三段渐变。
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := mask.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			gx := (float64(x) + 0.5) / k
			gy := (float64(y) + 0.5) / k
			t := ((gx-gradX0)/(gradX1-gradX0) + (gy-gradY0)/(gradY1-gradY0)) / 2
			blendOver(img, x, y, iconColor(t), uint32(a)*0x101)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// annulus 以多边形近似一段圆环（a0 到 a1 走长弧，角度递减）。
func annulus(z *vector.Rasterizer, cx, cy, r, sw, a0, a1 float64) {
	ro, ri := r+sw/2, r-sw/2
	end := a1 - 360
	steps := int(math.Ceil((a0 - end) / 0.25))
	pt := func(rad, radius float64) (float64, float64) {
		return cx + radius*math.Cos(deg(rad)), cy + radius*math.Sin(deg(rad))
	}
	for i := 0; i <= steps; i++ {
		a := a0 + (end-a0)*float64(i)/float64(steps)
		x, y := pt(a, ro)
		if i == 0 {
			z.MoveTo(float32(x), float32(y))
		} else {
			z.LineTo(float32(x), float32(y))
		}
	}
	for i := steps; i >= 0; i-- {
		a := a0 + (end-a0)*float64(i)/float64(steps)
		x, y := pt(a, ri)
		z.LineTo(float32(x), float32(y))
	}
	z.ClosePath()
}

func deg(d float64) float64 { return d * math.Pi / 180 }

// iconColor 返回渐变在 t 处的颜色（sRGB 线性插值，和 SVG 一致）。
func iconColor(t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	for i := 1; i < len(iconStops); i++ {
		if t <= iconStops[i].at {
			a, b := iconStops[i-1], iconStops[i]
			u := (t - a.at) / (b.at - a.at)
			return lerpColor(a.rgb, b.rgb, u)
		}
	}
	return hexColor(iconStops[len(iconStops)-1].rgb)
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

// zapOver 把形状以 Over 画到遮罩上（用于把多个形状并成同一字形）。
func zapOver(mask *image.Alpha, z *vector.Rasterizer) {
	z.DrawOp = draw.Over
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
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

// circle 画圆。
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
