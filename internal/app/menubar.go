package app

import (
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/getlantern/systray"
)

var exitProcess = os.Exit

func RunMenuBar(opts RunOptions) error {
	SetVerboseLogging(opts.Verbose)

	supervisor := newChildSupervisor(opts)
	var stopWakeObserver sync.Once
	var stopWake func()
	var shutdownOnce sync.Once
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	shutdown := func() {
		shutdownOnce.Do(func() {
			stopWakeObserver.Do(func() {
				if stopWake != nil {
					stopWake()
				}
			})
			supervisor.close()
		})
	}

	systray.Run(func() {
		icon := menuBarIcon()
		systray.SetTemplateIcon(icon, icon)
		systray.SetTitle("")
		systray.SetTooltip("my-streamdeck")

		if err := supervisor.start(); err != nil {
			log.Printf("child supervisor: start failed: %v", err)
		}

		wakeStop, err := startWakeObserver(func() {
			debugf("wake observer: notification received goroutines=%d", runtime.NumGoroutine())
			if err := supervisor.restart(); err != nil {
				log.Printf("child supervisor: restart after wake failed: %v", err)
			}
		})
		if err == nil {
			stopWake = wakeStop
		}

		quitItem := systray.AddMenuItem("Quit", "Quit my-streamdeck")
		go func() {
			<-quitItem.ClickedCh
			shutdown()
			systray.Quit()
		}()

		go func() {
			<-signalCh
			shutdown()
			systray.Quit()
		}()
	}, func() {
		shutdown()
		exitProcess(0)
	})

	return nil
}
