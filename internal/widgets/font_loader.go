package widgets

import (
	"os"

	"golang.org/x/image/font"
)

var hangulFontCandidates = []string{
	"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
	"/System/Library/Fonts/Supplemental/NotoSansGothic-Regular.ttf",
	"/Library/Fonts/Arial Unicode.ttf",
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
}

func newFaceFromFile(path string, size float64) (font.Face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return newFace(data, size)
}

func newHangulCapableFace(size float64) (font.Face, error) {
	var lastErr error
	for _, path := range hangulFontCandidates {
		face, err := newFaceFromFile(path, size)
		if err == nil {
			if faceSupportsRunes(face, []rune{'가', '스', 'A', '1'}) {
				return face, nil
			}
			_ = face.Close()
			lastErr = os.ErrInvalid
			continue
		}
		lastErr = err
	}
	return nil, lastErr
}

func faceSupportsRunes(face font.Face, runes []rune) bool {
	for _, r := range runes {
		if _, ok := face.GlyphAdvance(r); !ok {
			return false
		}
	}
	return true
}
