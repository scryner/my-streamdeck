package app

import (
	"fmt"
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
	// Keep intent across wake-triggered worker restarts, but start OFF in each
	// new menu bar session. The directory is private and removed on app exit.
	directory, err := os.MkdirTemp("", "my-streamdeck-session-*")
	if err != nil {
		return fmt.Errorf("create caffeinate session: %w", err)
	}
	opts.caffeinateSessionDir = directory

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
			_ = os.RemoveAll(directory)
		})
	}
	restartChild := func(reason string) {
		if err := supervisor.restart(); err != nil {
			log.Printf("child supervisor: restart after %s failed: %v", reason, err)
		}
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
			restartChild("wake")
		})
		if err == nil {
			stopWake = wakeStop
		}

		restartItem := systray.AddMenuItem("Restart", "Restart child process")
		quitItem := systray.AddMenuItem("Quit", "Quit my-streamdeck")
		go func() {
			for range restartItem.ClickedCh {
				debugf("menu bar: restart requested goroutines=%d", runtime.NumGoroutine())
				restartChild("menu request")
			}
		}()

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
