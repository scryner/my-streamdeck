package app

/*
#cgo darwin CFLAGS: -DDARWIN -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

extern void myStreamDeckHandleWake(void);

@interface MyStreamDeckWakeObserver : NSObject
@end

@implementation MyStreamDeckWakeObserver
- (void)handleWake:(NSNotification *)notification {
	(void)notification;
	myStreamDeckHandleWake();
}
@end

static MyStreamDeckWakeObserver *myStreamDeckWakeObserver = nil;

static void registerWakeObserver(void) {
	if (myStreamDeckWakeObserver != nil) {
		return;
	}

	myStreamDeckWakeObserver = [MyStreamDeckWakeObserver new];
	[[[NSWorkspace sharedWorkspace] notificationCenter]
		addObserver:myStreamDeckWakeObserver
		   selector:@selector(handleWake:)
		       name:NSWorkspaceDidWakeNotification
		     object:nil];
}

static void unregisterWakeObserver(void) {
	if (myStreamDeckWakeObserver == nil) {
		return;
	}

	[[[NSWorkspace sharedWorkspace] notificationCenter]
		removeObserver:myStreamDeckWakeObserver
		          name:NSWorkspaceDidWakeNotification
		        object:nil];
	myStreamDeckWakeObserver = nil;
}
*/
import "C"

import "sync"

var (
	wakeHandlerMu sync.Mutex
	wakeHandler   func()
)

func startWakeObserver(fn func()) (func(), error) {
	wakeHandlerMu.Lock()
	wakeHandler = fn
	wakeHandlerMu.Unlock()

	C.registerWakeObserver()
	return func() {
		C.unregisterWakeObserver()
		wakeHandlerMu.Lock()
		wakeHandler = nil
		wakeHandlerMu.Unlock()
	}, nil
}
