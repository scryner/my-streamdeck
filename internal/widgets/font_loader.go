package widgets

import (
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
)

var hangulFontCandidates = []string{
	"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
	"/System/Library/Fonts/Supplemental/NotoSansGothic-Regular.ttf",
	"/Library/Fonts/Arial Unicode.ttf",
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
}

const (
	fontCacheKeyGoBold = "embedded:gobold"
	fontCacheKeyGoMono = "embedded:gomono"
)

var (
	fontCacheMu     sync.RWMutex
	parsedFontCache = map[string]*opentype.Font{}
)

func newFaceFromFile(path string, size float64) (font.Face, error) {
	parsed, err := cachedParsedFont("file:"+path, func() ([]byte, error) {
		return os.ReadFile(path)
	})
	if err != nil {
		return nil, err
	}
	return newFaceFromParsedFont(parsed, size)
}

func newFace(ttf []byte, size float64) (font.Face, error) {
	if cacheKey, ok := embeddedFontCacheKey(ttf); ok {
		parsed, err := cachedParsedFont(cacheKey, func() ([]byte, error) {
			return ttf, nil
		})
		if err != nil {
			return nil, err
		}
		return newFaceFromParsedFont(parsed, size)
	}

	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return newFaceFromParsedFont(parsed, size)
}

func newFaceFromParsedFont(parsed *opentype.Font, size float64) (font.Face, error) {
	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func cachedParsedFont(cacheKey string, load func() ([]byte, error)) (*opentype.Font, error) {
	fontCacheMu.RLock()
	parsed := parsedFontCache[cacheKey]
	fontCacheMu.RUnlock()
	if parsed != nil {
		return parsed, nil
	}

	data, err := load()
	if err != nil {
		return nil, err
	}

	parsed, err = opentype.Parse(data)
	if err != nil {
		return nil, err
	}

	fontCacheMu.Lock()
	if cached := parsedFontCache[cacheKey]; cached != nil {
		fontCacheMu.Unlock()
		return cached, nil
	}
	parsedFontCache[cacheKey] = parsed
	fontCacheMu.Unlock()
	return parsed, nil
}

func embeddedFontCacheKey(ttf []byte) (string, bool) {
	switch {
	case sameFontData(ttf, gobold.TTF):
		return fontCacheKeyGoBold, true
	case sameFontData(ttf, gomono.TTF):
		return fontCacheKeyGoMono, true
	default:
		return "", false
	}
}

func sameFontData(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
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
