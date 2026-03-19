//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"math"
	"os"
	"path/filepath"

	"github.com/kettek/apng"
)

const (
	canvasSize = 120
	frameCount = 12
)

func main() {
	frames := make([]*image.NRGBA, 0, frameCount)
	for i := range frameCount {
		frames = append(frames, renderFrame(i, frameCount))
	}

	if err := os.MkdirAll("examples", 0o755); err != nil {
		panic(err)
	}

	if err := writeGIF(filepath.Join("examples", "sample-animated.gif"), frames); err != nil {
		panic(err)
	}
	if err := writeAPNG(filepath.Join("examples", "sample-animated.apng"), frames); err != nil {
		panic(err)
	}
}

func writeGIF(path string, frames []*image.NRGBA) error {
	anim := &gif.GIF{
		Image: make([]*image.Paletted, 0, len(frames)),
		Delay: make([]int, 0, len(frames)),
	}

	for _, frame := range frames {
		paletted := image.NewPaletted(frame.Bounds(), palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, frame.Bounds(), frame, frame.Bounds().Min)
		anim.Image = append(anim.Image, paletted)
		anim.Delay = append(anim.Delay, 8)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return gif.EncodeAll(file, anim)
}

func writeAPNG(path string, frames []*image.NRGBA) error {
	anim := apng.APNG{
		Frames:    make([]apng.Frame, 0, len(frames)),
		LoopCount: 0,
	}

	for _, frame := range frames {
		anim.Frames = append(anim.Frames, apng.Frame{
			Image:            frame,
			DelayNumerator:   1,
			DelayDenominator: 12,
			DisposeOp:        apng.DISPOSE_OP_NONE,
			BlendOp:          apng.BLEND_OP_SOURCE,
		})
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return apng.Encode(file, anim)
}

func renderFrame(index, total int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, canvasSize, canvasSize))

	progress := float64(index) / float64(total)
	bgTop := color.NRGBA{R: 17, G: 24, B: 39, A: 255}
	bgBottom := color.NRGBA{R: 30, G: 41, B: 59, A: 255}
	for y := range canvasSize {
		t := float64(y) / float64(canvasSize-1)
		row := mix(bgTop, bgBottom, t)
		for x := range canvasSize {
			img.SetNRGBA(x, y, row)
		}
	}

	cx := 20 + int(progress*80)
	cy := 60
	fillCircle(img, cx, cy, 16, color.NRGBA{R: 255, G: 106, B: 0, A: 255})

	pulse := 12 + 8*math.Sin(progress*2*math.Pi)
	strokeCircle(img, 60, 60, pulse, 3, color.NRGBA{R: 0, G: 212, B: 255, A: 220})

	barAngle := progress * 2 * math.Pi
	drawBar(img, 60, 60, 34, 10, barAngle, color.NRGBA{R: 255, G: 255, B: 255, A: 240})

	return img
}

func fillCircle(img *image.NRGBA, cx, cy, radius int, c color.NRGBA) {
	r2 := radius * radius
	for y := cy - radius; y <= cy+radius; y++ {
		if y < 0 || y >= canvasSize {
			continue
		}
		for x := cx - radius; x <= cx+radius; x++ {
			if x < 0 || x >= canvasSize {
				continue
			}
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= r2 {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func strokeCircle(img *image.NRGBA, cx, cy int, radius float64, thickness int, c color.NRGBA) {
	inner := radius - float64(thickness)
	outer := radius
	for y := range canvasSize {
		for x := range canvasSize {
			dx := float64(x - cx)
			dy := float64(y - cy)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist >= inner && dist <= outer {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func drawBar(img *image.NRGBA, cx, cy int, radius float64, length float64, angle float64, c color.NRGBA) {
	dx := math.Cos(angle)
	dy := math.Sin(angle)
	px := -dy
	py := dx

	startX := float64(cx) + dx*(radius-8)
	startY := float64(cy) + dy*(radius-8)
	endX := float64(cx) + dx*(radius+length)
	endY := float64(cy) + dy*(radius+length)

	for y := range canvasSize {
		for x := range canvasSize {
			if distanceToSegment(float64(x), float64(y), startX, startY, endX, endY) <= 3.5 {
				proj := (float64(x)-startX)*px + (float64(y)-startY)*py
				if math.Abs(proj) <= 3.5 {
					img.SetNRGBA(x, y, c)
				}
			}
		}
	}
}

func distanceToSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	if dx == 0 && dy == 0 {
		return math.Hypot(px-x1, py-y1)
	}

	t := ((px-x1)*dx + (py-y1)*dy) / (dx*dx + dy*dy)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}

	projX := x1 + t*dx
	projY := y1 + t*dy
	return math.Hypot(px-projX, py-projY)
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}
