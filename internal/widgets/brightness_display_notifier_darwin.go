package widgets

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics

#include <CoreGraphics/CGDisplayConfiguration.h>
#include <stdint.h>
#include <stdlib.h>

extern void myStreamDeckHandleDisplayChange(uintptr_t token);

typedef struct {
	uintptr_t token;
} MyStreamDeckDisplayObserver;

static void myStreamDeckDisplayReconfigurationCallback(CGDirectDisplayID display, CGDisplayChangeSummaryFlags flags, void *userInfo) {
	(void)display;
	(void)flags;
	MyStreamDeckDisplayObserver *observer = (MyStreamDeckDisplayObserver *)userInfo;
	if (observer == NULL) {
		return;
	}
	myStreamDeckHandleDisplayChange(observer->token);
}

static MyStreamDeckDisplayObserver *myStreamDeckStartDisplayObserver(uintptr_t token) {
	MyStreamDeckDisplayObserver *observer = (MyStreamDeckDisplayObserver *)calloc(1, sizeof(MyStreamDeckDisplayObserver));
	if (observer == NULL) {
		return NULL;
	}
	observer->token = token;
	if (CGDisplayRegisterReconfigurationCallback(myStreamDeckDisplayReconfigurationCallback, observer) != kCGErrorSuccess) {
		free(observer);
		return NULL;
	}
	return observer;
}

static void myStreamDeckStopDisplayObserver(MyStreamDeckDisplayObserver *observer) {
	if (observer == NULL) {
		return;
	}
	CGDisplayRemoveReconfigurationCallback(myStreamDeckDisplayReconfigurationCallback, observer);
	free(observer);
}
*/
import "C"

import (
	"fmt"
	"sync"
)

var (
	displayObserverMu       sync.Mutex
	displayObserverHandlers         = map[uintptr]func(){}
	nextDisplayObserverID   uintptr = 1
)

func startDisplayObserver(fn func()) (func(), error) {
	if fn == nil {
		return func() {}, nil
	}

	displayObserverMu.Lock()
	token := nextDisplayObserverID
	nextDisplayObserverID++
	displayObserverHandlers[token] = fn
	displayObserverMu.Unlock()

	observer := C.myStreamDeckStartDisplayObserver(C.uintptr_t(token))
	if observer == nil {
		displayObserverMu.Lock()
		delete(displayObserverHandlers, token)
		displayObserverMu.Unlock()
		return nil, fmt.Errorf("start display observer: register display reconfiguration callback")
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			displayObserverMu.Lock()
			delete(displayObserverHandlers, token)
			displayObserverMu.Unlock()
			C.myStreamDeckStopDisplayObserver(observer)
		})
	}, nil
}

//export myStreamDeckHandleDisplayChange
func myStreamDeckHandleDisplayChange(token C.uintptr_t) {
	displayObserverMu.Lock()
	handler := displayObserverHandlers[uintptr(token)]
	displayObserverMu.Unlock()
	if handler != nil {
		handler()
	}
}
