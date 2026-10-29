//go:build darwin && cgo

package widgets

/*
#include <stdint.h>
*/
import "C"

//export myStreamDeckCaffeinateSessionChanged
func myStreamDeckCaffeinateSessionChanged(token C.uintptr_t, event C.int) {
	caffeinateSessionObservers.Lock()
	handler := caffeinateSessionObservers.handlers[uintptr(token)]
	caffeinateSessionObservers.Unlock()
	if handler != nil {
		handler(caffeinateLockEvent(event))
	}
}
