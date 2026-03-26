package app

/*
 */
import "C"

import (
	"runtime"
)

//export myStreamDeckHandleWake
func myStreamDeckHandleWake() {
	wakeHandlerMu.Lock()
	handler := wakeHandler
	wakeHandlerMu.Unlock()
	debugf("wake observer: callback entered handlerSet=%t goroutines=%d", handler != nil, runtime.NumGoroutine())

	if handler != nil {
		go handler()
	}
}
