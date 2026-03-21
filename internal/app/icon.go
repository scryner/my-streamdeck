package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

var (
	menuBarIconOnce sync.Once
	menuBarIconPNG  []byte
)

func menuBarIcon() []byte {
	menuBarIconOnce.Do(func() {
		menuBarIconPNG = buildMenuBarIconPNG()
	})
	return menuBarIconPNG
}

func buildMenuBarIconPNG() []byte {
	const (
		size         = 64
		samplesPerAx = 4
	)

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	totalSamples := samplesPerAx * samplesPerAx
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			covered := 0
			for sy := 0; sy < samplesPerAx; sy++ {
				for sx := 0; sx < samplesPerAx; sx++ {
					px := float64(x) + (float64(sx)+0.5)/samplesPerAx
					py := float64(y) + (float64(sy)+0.5)/samplesPerAx
					if isMenuBarIconFilled(px, py) {
						covered++
					}
				}
			}
			if covered == 0 {
				continue
			}
			alpha := uint8(math.Round(float64(covered) * 255 / float64(totalSamples)))
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: alpha})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func isMenuBarIconFilled(x, y float64) bool {
	return pointInArcStroke(x, y, 31.5, 32, 25.5, 7.5, 34, 332) ||
		pointInSquareStrokeSegment(x, y, point{x: 24, y: 20.5}, point{x: 24, y: 46.5}, 7.2) ||
		pointInSquareStrokeSegment(x, y, point{x: 24, y: 20.5}, point{x: 55, y: 39.2}, 7.2) ||
		pointInSquareStrokeSegment(x, y, point{x: 24, y: 46.5}, point{x: 43.5, y: 34.5}, 7.2)
}

type point struct {
	x float64
	y float64
}

func pointInArcStroke(x, y, cx, cy, radius, width, startDeg, endDeg float64) bool {
	dx := x - cx
	dy := y - cy
	dist := math.Hypot(dx, dy)
	if dist < radius-width/2 || dist > radius+width/2 {
		return false
	}

	angle := math.Mod(math.Atan2(dy, dx)*180/math.Pi+360, 360)
	if startDeg <= endDeg {
		return angle >= startDeg && angle <= endDeg
	}
	return angle >= startDeg || angle <= endDeg
}

func pointInSquareStrokeSegment(x, y float64, a, b point, width float64) bool {
	abx := b.x - a.x
	aby := b.y - a.y
	apx := x - a.x
	apy := y - a.y
	denom := abx*abx + aby*aby
	if denom == 0 {
		return math.Hypot(apx, apy) <= width/2
	}

	length := math.Sqrt(denom)
	t := (apx*abx + apy*aby) / denom
	capPad := (width / 2) / length
	if t < -capPad || t > 1+capPad {
		return false
	}
	projX := a.x + t*abx
	projY := a.y + t*aby
	return math.Hypot(x-projX, y-projY) <= width/2
}
