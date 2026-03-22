package widgets

import "github.com/scryner/my-streamdeck/internal/deckbutton"

// ButtonWidget represents a key-bound Stream Deck widget.
type ButtonWidget interface {
	Button() deckbutton.Button
}
