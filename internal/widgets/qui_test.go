package widgets

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

func TestQuiFetchSnapshotAggregatesActiveInstances(t *testing.T) {
	t.Parallel()

	const apiKey = "test-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			t.Fatalf("unexpected api key header: %q", r.Header.Get("X-API-Key"))
		}

		switch r.URL.Path {
		case "/api/instances":
			_, _ = w.Write([]byte(`[
				{"id":1,"isActive":true,"connected":true},
				{"id":2,"isActive":true,"connected":true},
				{"id":3,"isActive":false,"connected":true},
				{"id":4,"isActive":true,"connected":false}
			]`))
		case "/api/instances/1/torrents":
			_, _ = w.Write([]byte(`{"counts":{"status":{"downloading":2,"completed":5,"seeding":3}}}`))
		case "/api/instances/2/torrents":
			_, _ = w.Write([]byte(`{"counts":{"status":{"downloading":1,"completed":4,"seeding":6}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	widget, err := NewQuiWidget(QuiWidgetOptions{
		Key:        streamdeck.KEY_8,
		Size:       DefaultClockWidgetSize,
		BaseURL:    server.URL,
		APIKey:     apiKey,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewQuiWidget: %v", err)
	}

	snapshot, err := widget.source.fetchSnapshot(context.Background(), server.URL, apiKey)
	if err != nil {
		t.Fatalf("fetchSnapshot: %v", err)
	}

	if snapshot.Downloading != 3 || snapshot.Completed != 9 || snapshot.Seeding != 9 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestQuiWidgetButtonOpensInstancesPage(t *testing.T) {
	t.Parallel()

	var opened string
	widget, err := NewQuiWidget(QuiWidgetOptions{
		Key:     streamdeck.KEY_8,
		Size:    DefaultClockWidgetSize,
		BaseURL: "https://example.test/",
		APIKey:  "test-key",
		Open: func(_ context.Context, url string) error {
			opened = url
			return nil
		},
		Fetch: func(context.Context, string, string) (quiSnapshot, error) {
			return quiSnapshot{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuiWidget: %v", err)
	}

	button := widget.Button()
	if button.OnPress == nil {
		t.Fatal("expected qui button to define OnPress handler")
	}
	if err := button.OnPress(nil, nil); err != nil {
		t.Fatalf("OnPress: %v", err)
	}
	if opened != "https://example.test/instances" {
		t.Fatalf("unexpected opened url: %q", opened)
	}
}

func TestQuiWidgetRendersExpectedBounds(t *testing.T) {
	t.Parallel()

	widget, err := NewQuiWidget(QuiWidgetOptions{
		Key:     streamdeck.KEY_8,
		Size:    DefaultClockWidgetSize,
		BaseURL: "https://example.test",
		APIKey:  "test-key",
		Fetch: func(context.Context, string, string) (quiSnapshot, error) {
			return quiSnapshot{
				Downloading: 10,
				Completed:   24,
				Seeding:     31,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuiWidget: %v", err)
	}

	frame, err := widget.Button().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}
	if !frame.Bounds().Eq(image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected bounds: %v", frame.Bounds())
	}

	visiblePixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x6000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected qui widget to render visible content")
	}
}

func TestQuiWidgetShowsNoSignalOnFetchFailure(t *testing.T) {
	t.Parallel()

	widget, err := NewQuiWidget(QuiWidgetOptions{
		Key:     streamdeck.KEY_8,
		Size:    DefaultClockWidgetSize,
		BaseURL: "https://example.test",
		APIKey:  "test-key",
		Fetch: func(context.Context, string, string) (quiSnapshot, error) {
			return quiSnapshot{}, fmt.Errorf("dial error")
		},
	})
	if err != nil {
		t.Fatalf("NewQuiWidget: %v", err)
	}

	frame, err := widget.Button().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	center := DefaultClockWidgetSize / 2
	visiblePixels := 0
	for y := center - 12; y <= center+12; y++ {
		for x := center - 50; x <= center+50; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x6000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected no signal message to render near center")
	}
}

func TestQuiWidgetRetriesAfterInitialFailure(t *testing.T) {
	t.Parallel()

	fetchCalls := 0
	widget, err := NewQuiWidget(QuiWidgetOptions{
		Key:     streamdeck.KEY_8,
		Size:    DefaultClockWidgetSize,
		BaseURL: "https://example.test",
		APIKey:  "test-key",
		Fetch: func(context.Context, string, string) (quiSnapshot, error) {
			fetchCalls++
			if fetchCalls == 1 {
				return quiSnapshot{}, fmt.Errorf("dial error")
			}
			return quiSnapshot{
				Downloading: 2,
				Completed:   3,
				Seeding:     5,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuiWidget: %v", err)
	}

	source := widget.source

	if _, err := source.FrameAt(context.Background(), 0); err != nil {
		t.Fatalf("initial FrameAt: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected 1 fetch after initial frame, got %d", fetchCalls)
	}
	if !source.isSignalLost() {
		t.Fatal("expected signalLost after initial fetch failure")
	}
	if source.shouldRefresh() {
		t.Fatal("expected failed fetch to wait until retry interval before refreshing again")
	}

	source.mu.Lock()
	source.lastFetch = time.Now().Add(-quiWidgetUpdateInterval - time.Second)
	source.mu.Unlock()

	if !source.shouldRefresh() {
		t.Fatal("expected refresh after retry interval elapsed")
	}
	if _, err := source.FrameAt(context.Background(), 0); err != nil {
		t.Fatalf("retry FrameAt: %v", err)
	}
	if fetchCalls != 2 {
		t.Fatalf("expected retry fetch after interval, got %d calls", fetchCalls)
	}
	if source.isSignalLost() {
		t.Fatal("expected signalLost cleared after successful retry")
	}
	snapshot := source.cachedSnapshot()
	if snapshot.Downloading != 2 || snapshot.Completed != 3 || snapshot.Seeding != 5 {
		t.Fatalf("unexpected snapshot after retry: %+v", snapshot)
	}
}
