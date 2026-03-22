package decktouch

import (
	"errors"
	"image"
	"testing"

	"rafaelmartins.com/p/streamdeck"
)

func TestNewWidgetValidatesID(t *testing.T) {
	t.Parallel()

	widget, err := NewWidget(WIDGET_3)
	if err != nil {
		t.Fatalf("NewWidget: %v", err)
	}
	if widget.ID != WIDGET_3 {
		t.Fatalf("expected widget id %v, got %v", WIDGET_3, widget.ID)
	}
}

func TestNewWidgetRejectsInvalidID(t *testing.T) {
	t.Parallel()

	_, err := NewWidget(0)
	if !errors.Is(err, ErrWidgetIDInvalid) {
		t.Fatalf("expected ErrWidgetIDInvalid, got %v", err)
	}
}

func TestWidgetIDDialMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   WidgetID
		dial streamdeck.DialID
	}{
		{WIDGET_1, streamdeck.DIAL_1},
		{WIDGET_2, streamdeck.DIAL_2},
		{WIDGET_3, streamdeck.DIAL_3},
		{WIDGET_4, streamdeck.DIAL_4},
	}

	for _, tc := range tests {
		if got := tc.id.DialID(); got != tc.dial {
			t.Fatalf("dial mapping mismatch for %s: got %s want %s", tc.id, got, tc.dial)
		}
	}
}

func TestTouchStripRectSplitsIntoQuarters(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 800, 100)
	tests := []struct {
		id   WidgetID
		rect image.Rectangle
	}{
		{WIDGET_1, image.Rect(0, 0, 200, 100)},
		{WIDGET_2, image.Rect(200, 0, 400, 100)},
		{WIDGET_3, image.Rect(400, 0, 600, 100)},
		{WIDGET_4, image.Rect(600, 0, 800, 100)},
	}

	for _, tc := range tests {
		if got := tc.id.TouchStripRect(bounds); got != tc.rect {
			t.Fatalf("rect mismatch for %s: got %v want %v", tc.id, got, tc.rect)
		}
	}
}

func TestWidgetLocalPointUsesLocalCoordinates(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 800, 100)
	widget, err := NewWidget(WIDGET_2)
	if err != nil {
		t.Fatalf("NewWidget: %v", err)
	}

	local, ok := widget.LocalPoint(bounds, image.Pt(250, 40))
	if !ok {
		t.Fatal("expected point to belong to widget")
	}
	if local != image.Pt(50, 40) {
		t.Fatalf("unexpected local point: got %v want %v", local, image.Pt(50, 40))
	}
}

func TestWidgetIDFromPoint(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 800, 100)
	id, ok := WidgetIDFromPoint(bounds, image.Pt(799, 50))
	if !ok {
		t.Fatal("expected point mapping to succeed")
	}
	if id != WIDGET_4 {
		t.Fatalf("unexpected widget id: got %s want %s", id, WIDGET_4)
	}
}
