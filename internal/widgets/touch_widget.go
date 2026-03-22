package widgets

import "github.com/scryner/my-streamdeck/internal/decktouch"

// TouchWidget represents a touch-strip quarter paired with a dial.
type TouchWidget interface {
	Touch() decktouch.Widget
}
