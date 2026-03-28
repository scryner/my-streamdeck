package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/scryner/my-streamdeck/internal/widgets"
)

func RunSpawnWorker(opts RunOptions) error {
	SetVerboseLogging(opts.Verbose)
	widgets.SetVerboseLogging(opts.Verbose)

	stopPprof, err := startPprofServer(opts.EnablePprof)
	if err != nil {
		return fmt.Errorf("start pprof server: %w", err)
	}

	runtimeInstance, err := StartRuntime()
	if err != nil {
		if stopPprof != nil {
			stopChildPprof(stopPprof)
		}
		return fmt.Errorf("start runtime: %w", err)
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case sig := <-signalCh:
		debugf("spawn worker: shutdown signal=%s", sig)
		if err := runtimeInstance.Close(); err != nil {
			log.Printf("spawn worker: close runtime: %v", err)
		}
		if stopPprof != nil {
			stopChildPprof(stopPprof)
		}
		return nil
	case err, ok := <-runtimeInstance.UnexpectedStop():
		if stopPprof != nil {
			stopChildPprof(stopPprof)
		}
		if !ok || err == nil {
			return nil
		}
		return fmt.Errorf("runtime stopped unexpectedly: %w", err)
	}
}

func stopChildPprof(stop func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := stop(ctx); err != nil {
		log.Printf("spawn worker: stop pprof server: %v", err)
	}
}
