package app

import (
	"bytes"
	"image/png"
	"testing"
)

func TestMenuBarIconPNGIsTransparentPNG(t *testing.T) {
	t.Parallel()

	img, err := png.Decode(bytes.NewReader(menuBarIcon()))
	if err != nil {
		t.Fatalf("decode menu bar icon: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 64 || bounds.Dy() != 64 {
		t.Fatalf("unexpected icon size: %dx%d", bounds.Dx(), bounds.Dy())
	}

	var hasOpaque bool
	var hasTransparent bool
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				hasTransparent = true
				continue
			}
			hasOpaque = true
		}
	}

	if !hasOpaque {
		t.Fatal("icon has no visible pixels")
	}
	if !hasTransparent {
		t.Fatal("icon background is not transparent")
	}
}
