package widgets

import (
	"context"
	"errors"
	"image"
	"math"
	"reflect"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

type fakeCaffeinateBackend struct {
	enabled      bool
	enableCalls  int
	disableCalls int
	sleepCalls   int
	enableErr    error
}

func (b *fakeCaffeinateBackend) Enable() error {
	if b.enableErr != nil {
		return b.enableErr
	}
	b.enabled = true
	b.enableCalls++
	return nil
}

func (b *fakeCaffeinateBackend) Disable() error {
	b.enabled = false
	b.disableCalls++
	return nil
}

func (b *fakeCaffeinateBackend) Sleep() error {
	b.sleepCalls++
	return nil
}

func (b *fakeCaffeinateBackend) Enabled() bool {
	return b.enabled
}

func TestCaffeinateWidgetToggleAndSleep(t *testing.T) {
	t.Parallel()

	backend := &fakeCaffeinateBackend{}
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	widget, err := NewCaffeinateWidget(CaffeinateWidgetOptions{
		Key:         streamdeck.KEY_7,
		Backend:     backend,
		LockMonitor: &fakeCaffeinateLockMonitor{},
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewCaffeinateWidget: %v", err)
	}

	if err := widget.toggle(); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	if !backend.enabled || backend.enableCalls != 1 {
		t.Fatalf("expected caffeinate to be enabled once, got %+v", backend)
	}
	if state := widget.controller.State(); !state.RequestedEnabled || state.ActiveDuration != 0 {
		t.Fatalf("unexpected initial state: %+v", state)
	}

	if err := widget.toggle(); err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	if backend.enabled || backend.disableCalls != 1 {
		t.Fatalf("expected caffeinate to be disabled once, got %+v", backend)
	}
	if widget.controller.State().ActiveDuration != 0 {
		t.Fatal("expected widget to clear enable time when toggled off")
	}

	now = now.Add(90 * time.Second)
	if err := widget.toggle(); err != nil {
		t.Fatalf("toggle on again: %v", err)
	}
	if err := widget.sleepNow(); err != nil {
		t.Fatalf("sleepNow: %v", err)
	}
	if backend.enabled {
		t.Fatal("expected backend to be disabled before sleep")
	}
	if backend.disableCalls != 2 || backend.sleepCalls != 1 {
		t.Fatalf("unexpected backend calls after sleep: %+v", backend)
	}
	if widget.controller.State().ActiveDuration != 0 {
		t.Fatal("expected widget to clear enable time before sleep")
	}
}

func TestCaffeinateSourceProgressResets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	source := &caffeinateSource{
		size: DefaultClockWidgetSize,
		now: func() time.Time {
			return now
		},
	}

	token := source.beginPress(now)
	now = now.Add(caffeinateTapDuration / 2)
	if got := source.holdProgress(now); got != 0 {
		t.Fatalf("expected no visible progress before tap threshold, got %f", got)
	}

	now = now.Add((caffeinateTapDuration / 2) + ((caffeinateGaugeDuration - caffeinateTapDuration) / 2))
	progress := source.holdProgress(now)
	if progress < 0.49 || progress > 0.51 {
		t.Fatalf("expected hold progress near 0.5, got %f", progress)
	}

	source.endPress(token)
	if got := source.holdProgress(now); got != 0 {
		t.Fatalf("expected hold progress reset to 0, got %f", got)
	}
}

func TestCaffeinateSourceFillTriggerMatchesRenderedFill(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	source := &caffeinateSource{
		size: DefaultClockWidgetSize,
		now: func() time.Time {
			return now
		},
	}

	token := source.beginPress(now)
	defer source.endPress(token)

	triggerAt := source.fillTriggerDuration()
	progressAtTrigger := easeOutProgress(source.holdProgress(now.Add(triggerAt)))
	fillStartY := int(math.Round(float64(source.size) * (1 - progressAtTrigger)))
	if fillStartY > 0 {
		t.Fatalf("expected gauge to visually fill by trigger time, got start y %d", fillStartY)
	}

	if triggerAt <= caffeinateTapDuration || triggerAt > caffeinateGaugeDuration {
		t.Fatalf("unexpected fill trigger duration: %s", triggerAt)
	}
}

func TestCaffeinateReleaseTogglePolicy(t *testing.T) {
	t.Parallel()

	if !shouldToggleOnRelease(caffeinateTapDuration) {
		t.Fatal("expected short tap to toggle")
	}
	if shouldToggleOnRelease(caffeinateTapDuration + time.Millisecond) {
		t.Fatal("expected partial hold release to preserve current state")
	}
}

func TestCaffeinateSourceNextFrameDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	backend := &fakeCaffeinateBackend{}
	source := &caffeinateSource{
		size:    DefaultClockWidgetSize,
		now:     func() time.Time { return now },
		state:   newCaffeinateController(backend, &fakeCaffeinateLockMonitor{}, nil, func() time.Time { return now }),
		updates: make(chan struct{}, 4),
	}

	if got := source.NextFrameDelay(); got != 0 {
		t.Fatalf("expected no periodic updates while idle, got %s", got)
	}

	token := source.beginPress(now)
	if got := source.NextFrameDelay(); got != time.Second/caffeinateWidgetFrameRate {
		t.Fatalf("expected hold updates at %s, got %s", time.Second/caffeinateWidgetFrameRate, got)
	}

	source.endPress(token)
	if got := source.NextFrameDelay(); got != 0 {
		t.Fatalf("expected no periodic updates after release, got %s", got)
	}

	if err := source.state.toggle(); err != nil {
		t.Fatal(err)
	}
	if got := source.NextFrameDelay(); got != time.Second {
		t.Fatalf("expected enabled updates at 1s, got %s", got)
	}

	if err := source.state.toggle(); err != nil {
		t.Fatal(err)
	}
	if got := source.NextFrameDelay(); got != 0 {
		t.Fatalf("expected no periodic updates after disable, got %s", got)
	}
}

func TestCaffeinateWidgetRendersExpectedBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	widget, err := NewCaffeinateWidget(CaffeinateWidgetOptions{
		Key: streamdeck.KEY_7,
		Now: func() time.Time {
			return now
		},
		Backend: &fakeCaffeinateBackend{},
	})
	if err != nil {
		t.Fatalf("NewCaffeinateWidget: %v", err)
	}

	button := widget.Button()
	if button.Animation == nil {
		t.Fatal("expected caffeinate widget to provide animation")
	}
	if button.Animation.FrameRate != caffeinateWidgetFrameRate {
		t.Fatalf("expected frame rate %d, got %d", caffeinateWidgetFrameRate, button.Animation.FrameRate)
	}

	frame, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}
	if !reflect.DeepEqual(frame.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected caffeinate bounds: %v", frame.Bounds())
	}
}

func TestCaffeinateElapsedParts(t *testing.T) {
	t.Parallel()

	minutes, seconds := caffeinateElapsedParts(123*time.Minute + 55*time.Second)
	if minutes != "123" || seconds != "55" {
		t.Fatalf("expected elapsed parts 123 and 55, got %q and %q", minutes, seconds)
	}

	minutes, seconds = caffeinateElapsedParts(-5 * time.Second)
	if minutes != "0" || seconds != "00" {
		t.Fatalf("expected negative durations to clamp to zero, got %q and %q", minutes, seconds)
	}
}

func TestCaffeinateSourceDistinguishesRequestedAndPausedStates(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	b := &fakeCaffeinateBackend{}
	m := &fakeCaffeinateLockMonitor{}
	w, err := NewCaffeinateWidget(CaffeinateWidgetOptions{
		Backend: b, LockMonitor: m, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.source.Close()
	ctx := context.Background()
	seen := make(map[uint64]string)
	check := func(want string) (uint64, image.Image) {
		t.Helper()
		state := w.controller.State()
		if state.statusText() != want {
			t.Fatalf("status = %q, want %q", state.statusText(), want)
		}
		sig, err := w.source.StateSignature(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if prior, ok := seen[sig]; ok {
			t.Fatalf("%s reused %s's rendering signature", want, prior)
		}
		seen[sig] = want
		frame, err := w.source.FrameAt(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		return sig, frame
	}
	check("OFF")
	if err := w.toggle(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	check("ON")
	m.set(true, nil)
	if err := refreshCaffeinate(w.controller); err != nil {
		t.Fatal(err)
	}
	pausedSignature, pausedFrame := check("ON / LOCK")
	now = now.Add(time.Hour)
	sig, err := w.source.StateSignature(ctx, 0)
	if err != nil || sig != pausedSignature {
		t.Fatal("paused timer changed rendering signature")
	}
	frame, err := w.source.FrameAt(ctx, 0)
	if err != nil || !reflect.DeepEqual(frame, pausedFrame) {
		t.Fatal("paused UI kept ticking")
	}
	if delay := w.source.NextFrameDelay(); delay != 0 {
		t.Fatalf("paused render polling = %s", delay)
	}
	m.set(false, errors.New("query failed"))
	if err := refreshCaffeinate(w.controller); err == nil {
		t.Fatal("expected query error")
	}
	check("ON / WAIT")
	m.set(false, nil)
	b.enableErr = errors.New("start failed")
	if err := refreshCaffeinate(w.controller); err == nil {
		t.Fatal("expected start error")
	}
	check("ON / RETRY")
}
