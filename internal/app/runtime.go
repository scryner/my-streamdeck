package app

import (
	"fmt"
	"image/color"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"github.com/scryner/my-streamdeck/internal/widgets"
	"rafaelmartins.com/p/streamdeck"
)

type Runtime struct {
	device     *streamdeck.Device
	controller *deckbutton.Controller
	stopCh     chan struct{}
	doneCh     chan struct{}
	closeOnce  sync.Once
}

func StartRuntime() (*Runtime, error) {
	cfg, exists, err := LoadConfig()
	if err != nil {
		log.Printf("config load failed, falling back to defaults: %v", err)
		cfg = DefaultConfig()
		exists = false
	}

	device, err := streamdeck.GetDevice("")
	if err != nil {
		return nil, err
	}
	if err := device.Open(); err != nil {
		return nil, err
	}

	buttons, usedKeys, err := buildButtons(cfg, exists, int(device.GetKeyCount()))
	if err != nil {
		_ = device.Close()
		return nil, err
	}

	if err := clearUnusedKeys(device, usedKeys); err != nil {
		_ = device.Close()
		return nil, err
	}

	controller := deckbutton.NewController(device)
	if err := controller.RegisterButtons(buttons...); err != nil {
		controller.Close()
		_ = device.Close()
		return nil, err
	}

	rt := &Runtime{
		device:     device,
		controller: controller,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	go rt.listen()
	return rt, nil
}

func (r *Runtime) listen() {
	defer close(r.doneCh)
	if err := r.device.Listen(nil); err != nil {
		select {
		case <-r.stopCh:
			return
		default:
			log.Printf("stream deck listener stopped: %v", err)
		}
	}
}

func (r *Runtime) Close() {
	r.closeOnce.Do(func() {
		close(r.stopCh)
		if r.controller != nil {
			r.controller.Close()
		}
		if r.device != nil && r.device.IsOpen() {
			if err := clearDisplays(r.device); err != nil {
				log.Printf("clear stream deck displays: %v", err)
			}
			_ = r.device.Close()
		}
		select {
		case <-r.doneCh:
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func buildButtons(cfg Config, configExists bool, maxKeys int) ([]deckbutton.Button, map[streamdeck.KeyID]struct{}, error) {
	if maxKeys <= 0 {
		return nil, nil, fmt.Errorf("stream deck has no keys")
	}

	settings := cfg.SettingsMap()
	widgetDefs := cfg.Widgets
	if len(widgetDefs) == 0 && !configExists {
		widgetDefs = DefaultConfig().Widgets
	}

	buttons := make([]deckbutton.Button, 0, minInt(maxKeys, len(widgetDefs)))
	usedKeys := make(map[streamdeck.KeyID]struct{}, maxKeys)
	var weatherWidget *widgets.WeatherWidget

	for _, def := range widgetDefs {
		if len(buttons) >= maxKeys {
			log.Printf("ignoring extra widget %q: device only has %d keys", def.Type, maxKeys)
			break
		}

		key := streamdeck.KEY_1 + streamdeck.KeyID(len(buttons))
		button, err := buildButtonForWidget(def, key, settings, &weatherWidget)
		if err != nil {
			log.Printf("skip widget %q: %v", def.Type, err)
			continue
		}

		buttons = append(buttons, button)
		usedKeys[key] = struct{}{}
	}

	return buttons, usedKeys, nil
}

func buildButtonForWidget(def WidgetConfig, key streamdeck.KeyID, settings map[string]string, weatherWidget **widgets.WeatherWidget) (deckbutton.Button, error) {
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
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	case "calendar":
		widget, err := widgets.NewCalendarWidget(widgets.CalendarWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	case "sysstat":
		widget, err := widgets.NewSysstatWidget(widgets.SysstatWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	case "network":
		iface := strings.TrimSpace(def.Interface)
		if iface == "" {
			return deckbutton.Button{}, fmt.Errorf("missing interface")
		}
		widget, err := widgets.NewNetstatWidget(widgets.NetstatWidgetOptions{
			Key:       key,
			Size:      widgets.DefaultClockWidgetSize,
			Interface: iface,
		})
		if err != nil {
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	case "weather.today", "weather.forecast":
		location := strings.TrimSpace(settings["weather.location"])
		if location == "" {
			return deckbutton.Button{}, fmt.Errorf("missing weather.location")
		}
		if *weatherWidget == nil {
			widget, err := widgets.NewWeatherWidget(widgets.WeatherWidgetOptions{
				Location: location,
				Size:     widgets.DefaultClockWidgetSize,
			})
			if err != nil {
				return deckbutton.Button{}, err
			}
			*weatherWidget = widget
		}
		if def.Type == "weather.today" {
			return (*weatherWidget).Today(key).Button(), nil
		}
		return (*weatherWidget).Forecast(key).Button(), nil
	case "caffeinate":
		widget, err := widgets.NewCaffeinateWidget(widgets.CaffeinateWidgetOptions{
			Key:  key,
			Size: widgets.DefaultClockWidgetSize,
		})
		if err != nil {
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	case "qui":
		token := strings.TrimSpace(settings["qui.access_token"])
		if token == "" {
			return deckbutton.Button{}, fmt.Errorf("missing qui.access_token")
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
			return deckbutton.Button{}, err
		}
		return widget.Button(), nil
	default:
		return deckbutton.Button{}, fmt.Errorf("unknown widget type")
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
