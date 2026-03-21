package app

/*
 */
import "C"

//export myStreamDeckHandleWake
func myStreamDeckHandleWake() {
	wakeHandlerMu.Lock()
	handler := wakeHandler
	wakeHandlerMu.Unlock()

	if handler != nil {
		go handler()
	}
}
