package app

import (
	"os"
	"sync"

	"github.com/getlantern/systray"
)

var exitProcess = os.Exit

func RunMenuBar() error {
	var (
		runtime *Runtime
		mu      sync.Mutex
	)

	systray.Run(func() {
		icon := menuBarIcon()
		systray.SetTemplateIcon(icon, icon)
		systray.SetTitle("")
		systray.SetTooltip("my-streamdeck")

		rt, err := StartRuntime()
		if err == nil {
			mu.Lock()
			runtime = rt
			mu.Unlock()
		}

		quitItem := systray.AddMenuItem("Quit", "Quit my-streamdeck")
		go func() {
			<-quitItem.ClickedCh
			mu.Lock()
			rt := runtime
			mu.Unlock()
			if rt != nil {
				go rt.Close()
			}
			systray.Quit()
		}()
	}, func() {
		exitProcess(0)
	})

	return nil
}
