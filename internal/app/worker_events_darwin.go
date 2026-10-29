//go:build darwin && cgo

package app

/*
#cgo darwin LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

static void myStreamDeckPumpWorkerEvents(void) {
	@autoreleasepool {
	CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.1, false);
	}
}
*/
import "C"

func pumpWorkerEvents() { C.myStreamDeckPumpWorkerEvents() }
