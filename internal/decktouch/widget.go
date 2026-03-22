package decktouch

import (
	"errors"
	"fmt"
	"image"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"rafaelmartins.com/p/streamdeck"
)

var ErrWidgetIDInvalid = errors.New("decktouch: invalid widget id")

// Animation reuses the same frame-source model as button widgets.
type Animation = deckbutton.Animation

// WidgetID identifies one of four touch-strip quarters paired with a dial.
type WidgetID byte

const (
	WIDGET_1 WidgetID = iota + 1
	WIDGET_2
	WIDGET_3
	WIDGET_4
)

// TouchHandler is called for touch events inside a widget's touch-strip area.
// Coordinates are local to the widget rectangle.
type TouchHandler func(d *streamdeck.Device, w *Widget, typ streamdeck.TouchStripTouchType, p image.Point) error

// SwipeHandler is called for swipe events inside a widget's touch-strip area.
// Coordinates are local to the widget rectangle.
type SwipeHandler func(d *streamdeck.Device, w *Widget, origin image.Point, destination image.Point) error

// DialPressHandler is called when the widget's mapped dial is pressed.
type DialPressHandler func(d *streamdeck.Device, w *Widget, dial *streamdeck.Dial) error

// DialRotateHandler is called after a short batching window when the widget's
// mapped dial rotation has settled. steps is the accumulated rotation count.
type DialRotateHandler func(d *streamdeck.Device, w *Widget, dial *streamdeck.Dial, steps int) error

// Widget represents one quarter of the touch strip and its paired dial.
type Widget struct {
	ID           WidgetID
	Animation    *Animation
	OnTouch      TouchHandler
	OnSwipe      SwipeHandler
	OnDialPress  DialPressHandler
	OnDialRotate DialRotateHandler
}

// NewWidget validates and returns a touch widget scaffold for the provided id.
func NewWidget(id WidgetID) (Widget, error) {
	if err := id.Validate(); err != nil {
		return Widget{}, err
	}
	return Widget{ID: id}, nil
}

// Validate reports whether the widget id maps to one of the four supported touch widgets.
func (id WidgetID) Validate() error {
	if id < WIDGET_1 || id > WIDGET_4 {
		return fmt.Errorf("%w: %d", ErrWidgetIDInvalid, id)
	}
	return nil
}

func (id WidgetID) String() string {
	return fmt.Sprintf("TOUCH_WIDGET_%d", id)
}

// DialID returns the physical dial paired with this touch widget.
func (id WidgetID) DialID() streamdeck.DialID {
	return streamdeck.DIAL_1 + streamdeck.DialID(id-WIDGET_1)
}

// TouchStripRect returns this widget's quarter of the full touch-strip bounds.
func (id WidgetID) TouchStripRect(bounds image.Rectangle) image.Rectangle {
	if bounds.Empty() {
		return bounds
	}

	index := int(id - WIDGET_1)
	width := bounds.Dx() / 4
	minX := bounds.Min.X + width*index
	maxX := minX + width
	if id == WIDGET_4 {
		maxX = bounds.Max.X
	}
	return image.Rect(minX, bounds.Min.Y, maxX, bounds.Max.Y)
}

// LocalPoint converts a touch-strip point into widget-local coordinates.
func (w Widget) LocalPoint(bounds image.Rectangle, p image.Point) (image.Point, bool) {
	rect := w.ID.TouchStripRect(bounds)
	if !p.In(rect) {
		return image.Point{}, false
	}
	return p.Sub(rect.Min), true
}

// WidgetIDFromPoint maps a touch-strip point to its owning touch widget.
func WidgetIDFromPoint(bounds image.Rectangle, p image.Point) (WidgetID, bool) {
	if bounds.Empty() || !p.In(bounds) {
		return 0, false
	}

	for id := WIDGET_1; id <= WIDGET_4; id++ {
		if p.In(id.TouchStripRect(bounds)) {
			return id, true
		}
	}
	return 0, false
}
