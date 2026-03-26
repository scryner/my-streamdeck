package app

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"github.com/scryner/my-streamdeck/internal/decktouch"
	"github.com/scryner/my-streamdeck/internal/widgets"
	"rafaelmartins.com/p/streamdeck"
)

type Runtime struct {
	id               uint64
	device           *streamdeck.Device
	controller       *deckbutton.Controller
	touchController  *decktouch.Controller
	stopCh           chan struct{}
	doneCh           chan struct{}
	unexpectedStopCh chan error
	startedAt        time.Time
	closeOnce        sync.Once
	closeErrMu       sync.Mutex
	closeErr         error
}

const (
	runtimeDeviceOpenAttempts      = 20
	runtimeDeviceOpenRetryInterval = 250 * time.Millisecond
	runtimeDisplayWakeDelay        = 150 * time.Millisecond
	runtimeBrightnessPercent       = 100
)

var (
	ErrRuntimeCloseTimedOut = errors.New("runtime close timed out")
	runtimeCloseTimeout     = 5 * time.Second
	runtimeIDSeq            atomic.Uint64
)

func StartRuntime() (*Runtime, error) {
	id := runtimeIDSeq.Add(1)
	debugf("runtime[%d] start: begin goroutines=%d", id, runtime.NumGoroutine())

	cfg, exists, err := LoadConfig()
	if err != nil {
		log.Printf("config load failed, falling back to defaults: %v", err)
		cfg = DefaultConfig()
		exists = false
	}

	device, err := openFreshDevice()
	if err != nil {
		log.Printf("runtime[%d] start: open device failed: %v", id, err)
		return nil, err
	}
	settings := cfg.SettingsMap()
	if err := device.SetBrightness(resolveBrightness(settings)); err != nil {
		_ = device.Close()
		log.Printf("runtime[%d] start: set brightness failed: %v", id, err)
		return nil, fmt.Errorf("set stream deck brightness: %w", err)
	}
	time.Sleep(runtimeDisplayWakeDelay)

	buttonWidgets, usedKeys, err := buildButtonWidgets(cfg, exists, int(device.GetKeyCount()))
	if err != nil {
		_ = device.Close()
		log.Printf("runtime[%d] start: build button widgets failed: %v", id, err)
		return nil, err
	}
	buttons := make([]deckbutton.Button, 0, len(buttonWidgets))
	for _, buttonWidget := range buttonWidgets {
		buttons = append(buttons, buttonWidget.Button())
	}

	if err := clearUnusedKeys(device, usedKeys); err != nil {
		_ = device.Close()
		log.Printf("runtime[%d] start: clear unused keys failed: %v", id, err)
		return nil, err
	}

	controller := deckbutton.NewController(device)
	if err := controller.RegisterButtons(buttons...); err != nil {
		controller.Close()
		_ = device.Close()
		log.Printf("runtime[%d] start: register buttons failed: %v", id, err)
		return nil, err
	}

	touchWidgets, err := buildTouchWidgets(cfg, device)
	if err != nil {
		controller.Close()
		_ = device.Close()
		log.Printf("runtime[%d] start: build touch widgets failed: %v", id, err)
		return nil, err
	}

	var touchController *decktouch.Controller
	if len(touchWidgets) > 0 {
		touchController, err = decktouch.NewController(device)
		if err != nil {
			controller.Close()
			_ = device.Close()
			log.Printf("runtime[%d] start: new touch controller failed: %v", id, err)
			return nil, err
		}

		touchDefs := make([]decktouch.Widget, 0, len(touchWidgets))
		for _, touchWidget := range touchWidgets {
			touchDefs = append(touchDefs, touchWidget.Touch())
		}
		if err := touchController.RegisterWidgets(touchDefs...); err != nil {
			touchController.Close()
			controller.Close()
			_ = device.Close()
			log.Printf("runtime[%d] start: register touch widgets failed: %v", id, err)
			return nil, err
		}
	}

	rt := &Runtime{
		id:               id,
		device:           device,
		controller:       controller,
		touchController:  touchController,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		unexpectedStopCh: make(chan error, 1),
		startedAt:        time.Now(),
	}
	debugf(
		"runtime[%d] start: ready device=%p buttons=%d touchWidgets=%d goroutines=%d",
		rt.id,
		rt.device,
		len(buttons),
		len(touchWidgets),
		runtime.NumGoroutine(),
	)
	go rt.listen()
	return rt, nil
}

func openFreshDevice() (*streamdeck.Device, error) {
	var lastErr error
	for attempt := 0; attempt < runtimeDeviceOpenAttempts; attempt++ {
		device, err := streamdeck.GetDevice("")
		if err == nil {
			err = device.Open()
		}
		if err == nil {
			return device, nil
		}
		lastErr = err
		time.Sleep(runtimeDeviceOpenRetryInterval)
	}

	return nil, fmt.Errorf("reopen stream deck after reset: %w", lastErr)
}

func resolveBrightness(settings map[string]string) byte {
	raw := strings.TrimSpace(settings["brightness"])
	if raw == "" {
		return runtimeBrightnessPercent
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("invalid brightness %q, using default %d", raw, runtimeBrightnessPercent)
		return runtimeBrightnessPercent
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return byte(value)
}

func (r *Runtime) listen() {
	defer close(r.doneCh)
	defer close(r.unexpectedStopCh)
	debugf("runtime[%d] listen: begin device=%p goroutines=%d", r.id, r.device, runtime.NumGoroutine())
	if err := r.device.Listen(nil); err != nil {
		stopping := false
		select {
		case <-r.stopCh:
			stopping = true
		default:
		}
		debugf(
			"runtime[%d] listen: exit err=%v stopping=%t uptime=%s goroutines=%d",
			r.id,
			err,
			stopping,
			time.Since(r.startedAt).Round(time.Millisecond),
			runtime.NumGoroutine(),
		)
		select {
		case <-r.stopCh:
			return
		default:
			select {
			case r.unexpectedStopCh <- err:
			default:
			}
			log.Printf("stream deck listener stopped: %v", err)
		}
		return
	}
	debugf(
		"runtime[%d] listen: exit clean uptime=%s goroutines=%d",
		r.id,
		time.Since(r.startedAt).Round(time.Millisecond),
		runtime.NumGoroutine(),
	)
}

func (r *Runtime) appendCloseErr(err error) {
	if err == nil {
		return
	}
	r.closeErrMu.Lock()
	defer r.closeErrMu.Unlock()
	r.closeErr = errors.Join(r.closeErr, err)
}

func (r *Runtime) currentCloseErr() error {
	r.closeErrMu.Lock()
	defer r.closeErrMu.Unlock()
	return r.closeErr
}

func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		debugf("runtime[%d] close: begin device=%p goroutines=%d", r.id, r.device, runtime.NumGoroutine())
		close(r.stopCh)
		if r.touchController != nil {
			r.touchController.Close()
		}
		if r.controller != nil {
			r.controller.Close()
		}
		if r.device != nil && r.device.IsOpen() {
			if err := clearDisplays(r.device); err != nil {
				log.Printf("clear stream deck displays: %v", err)
				r.appendCloseErr(fmt.Errorf("clear stream deck displays: %w", err))
			}
			if err := r.device.Close(); err != nil {
				log.Printf("close stream deck device: %v", err)
				r.appendCloseErr(fmt.Errorf("close stream deck device: %w", err))
			}
		}
	})

	waitedForDone := true
	timer := time.NewTimer(runtimeCloseTimeout)
	defer timer.Stop()
	select {
	case <-r.doneCh:
	case <-timer.C:
		waitedForDone = false
	}

	closeErr := r.currentCloseErr()
	if !waitedForDone {
		closeErr = errors.Join(closeErr, ErrRuntimeCloseTimedOut)
	}

	debugf(
		"runtime[%d] close: end waitedForDone=%t err=%v uptime=%s goroutines=%d",
		r.id,
		waitedForDone,
		closeErr,
		time.Since(r.startedAt).Round(time.Millisecond),
		runtime.NumGoroutine(),
	)
	return closeErr
}

func (r *Runtime) UnexpectedStop() <-chan error {
	return r.unexpectedStopCh
}

func buildTouchWidgets(cfg Config, device *streamdeck.Device) ([]widgets.TouchWidget, error) {
	if !device.GetTouchStripSupported() || device.GetDialCount() == 0 {
		return nil, nil
	}

	stripBounds, err := device.GetTouchStripImageRectangle()
	if err != nil {
		return nil, fmt.Errorf("get touch strip bounds: %w", err)
	}

	touchWidgetDefs := cfg.TouchWidgets
	if len(touchWidgetDefs) == 0 {
		touchWidgetDefs = DefaultConfig().TouchWidgets
	}

	maxTouchWidgets := minInt(4, int(device.GetDialCount()))
	touchWidgets := make([]widgets.TouchWidget, 0, minInt(maxTouchWidgets, len(touchWidgetDefs)))
	ids := []decktouch.WidgetID{
		decktouch.WIDGET_1,
		decktouch.WIDGET_2,
		decktouch.WIDGET_3,
		decktouch.WIDGET_4,
	}
	for _, def := range touchWidgetDefs {
		if len(touchWidgets) >= maxTouchWidgets {
			log.Printf("ignoring extra touch widget %q: device only supports %d touch widget slots", def.Type, maxTouchWidgets)
			break
		}

		id := ids[len(touchWidgets)]
		rect := id.TouchStripRect(stripBounds)
		touchWidget, err := buildTouchWidget(def, id, rect.Size())
		if err != nil {
			log.Printf("skip touch widget %q: %v", def.Type, err)
			continue
		}
		touchWidgets = append(touchWidgets, touchWidget)
	}

	return touchWidgets, nil
}

func buildTouchWidget(def TouchWidgetConfig, id decktouch.WidgetID, size image.Point) (widgets.TouchWidget, error) {
	switch def.Type {
	case "volume":
		return widgets.NewVolumeTouchWidget(widgets.VolumeTouchWidgetOptions{
			ID:   id,
			Size: size,
		})
	case "microphone":
		return widgets.NewMicrophoneTouchWidget(widgets.MicrophoneTouchWidgetOptions{
			ID:   id,
			Size: size,
		})
	case "brightness":
		return widgets.NewBrightnessTouchWidget(widgets.BrightnessTouchWidgetOptions{
			ID:   id,
			Size: size,
		})
	case "playback":
		return widgets.NewPlayTouchWidget(widgets.PlayTouchWidgetOptions{
			ID:   id,
			Size: size,
		})
	default:
		return nil, fmt.Errorf("unknown touch widget type %q", def.Type)
	}
}

func buildButtonWidgets(cfg Config, configExists bool, maxKeys int) ([]widgets.ButtonWidget, map[streamdeck.KeyID]struct{}, error) {
	if maxKeys <= 0 {
		return nil, nil, fmt.Errorf("stream deck has no keys")
	}

	settings := cfg.SettingsMap()
	buttonWidgetDefs := cfg.ButtonWidgets
	if len(buttonWidgetDefs) == 0 && !configExists {
		buttonWidgetDefs = DefaultConfig().ButtonWidgets
	}

	buttonWidgets := make([]widgets.ButtonWidget, 0, minInt(maxKeys, len(buttonWidgetDefs)))
	usedKeys := make(map[streamdeck.KeyID]struct{}, maxKeys)
	var weatherWidget *widgets.WeatherWidget

	for _, def := range buttonWidgetDefs {
		if len(buttonWidgets) >= maxKeys {
			log.Printf("ignoring extra widget %q: device only has %d keys", def.Type, maxKeys)
			break
		}

		key := streamdeck.KEY_1 + streamdeck.KeyID(len(buttonWidgets))
		buttonWidget, err := buildButtonWidget(def, key, settings, &weatherWidget)
		if err != nil {
			log.Printf("skip widget %q: %v", def.Type, err)
			continue
		}

		buttonWidgets = append(buttonWidgets, buttonWidget)
		usedKeys[key] = struct{}{}
	}

	return buttonWidgets, usedKeys, nil
}

func buildButtonWidget(def ButtonWidgetConfig, key streamdeck.KeyID, settings map[string]string, weatherWidget **widgets.WeatherWidget) (widgets.ButtonWidget, error) {
	switch def.Type {
	case "clock":
		opts := widgets.ClockWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		}
		if strings.EqualFold(def.First, "digital") {
			opts.InitialMode = widgets.ClockModeDigital
		}
		widget, err := widgets.NewClockWidget(opts)
		if err != nil {
			return nil, err
		}
		return widget, nil
	case "calendar":
		widget, err := widgets.NewCalendarWidget(widgets.CalendarWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return nil, err
		}
		return widget, nil
	case "sysstat":
		widget, err := widgets.NewSysstatWidget(widgets.SysstatWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return nil, err
		}
		return widget, nil
	case "network":
		iface := strings.TrimSpace(def.Interface)
		if iface == "" {
			return nil, fmt.Errorf("missing interface")
		}
		widget, err := widgets.NewNetstatWidget(widgets.NetstatWidgetOptions{
			Key:       key,
			Size:      widgets.DefaultClockWidgetSize,
			Interface: iface,
		})
		if err != nil {
			return nil, err
		}
		return widget, nil
	case "weather.today", "weather.forecast":
		location := strings.TrimSpace(settings["weather.location"])
		if location == "" {
			return nil, fmt.Errorf("missing weather.location")
		}
		if *weatherWidget == nil {
			widget, err := widgets.NewWeatherWidget(widgets.WeatherWidgetOptions{
				Location: location,
				Size:     widgets.DefaultClockWidgetSize,
			})
			if err != nil {
				return nil, err
			}
			*weatherWidget = widget
		}
		if def.Type == "weather.today" {
			return (*weatherWidget).Today(key), nil
		}
		return (*weatherWidget).Forecast(key), nil
	case "caffeinate":
		widget, err := widgets.NewCaffeinateWidget(widgets.CaffeinateWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return nil, err
		}
		return widget, nil
	case "qui":
		token := strings.TrimSpace(settings["qui.access_token"])
		if token == "" {
			return nil, fmt.Errorf("missing qui.access_token")
		}
		baseURL := strings.TrimSpace(settings["qui.base_url"])
		if baseURL == "" {
			baseURL = defaultQuiBaseURL
		}
		widget, err := widgets.NewQuiWidget(widgets.QuiWidgetOptions{
			Key:     key,
			Size:    widgets.DefaultClockWidgetSize,
			BaseURL: baseURL,
			APIKey:  token,
		})
		if err != nil {
			return nil, err
		}
		return widget, nil
	default:
		return nil, fmt.Errorf("unknown widget type")
	}
}

func clearUnusedKeys(device *streamdeck.Device, usedKeys map[streamdeck.KeyID]struct{}) error {
	black := color.RGBA{A: 255}
	return device.ForEachKey(func(key streamdeck.KeyID) error {
		if _, ok := usedKeys[key]; ok {
			return nil
		}
		return device.SetKeyColor(key, black)
	})
}

func clearDisplays(device *streamdeck.Device) error {
	if err := device.ForEachKey(device.ClearKey); err != nil {
		return err
	}
	if err := device.ForEachTouchPoint(device.ClearTouchPoint); err != nil {
		return err
	}
	if device.GetInfoBarSupported() {
		if err := device.ClearInfoBar(); err != nil {
			return err
		}
	}
	if device.GetTouchStripSupported() {
		if err := device.ClearTouchStrip(); err != nil {
			return err
		}
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
