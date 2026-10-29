//go:build darwin && cgo

package widgets

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa -framework CoreGraphics

#import <Cocoa/Cocoa.h>
#include <CoreGraphics/CGSession.h>
#include <stdint.h>

extern void myStreamDeckCaffeinateSessionChanged(uintptr_t token, int event);

static int caffeinateSessionFlag(CFDictionaryRef session, CFStringRef key) {
	CFTypeRef value = CFDictionaryGetValue(session, key);
	if (value == NULL) return -1;
	if (CFGetTypeID(value) != CFBooleanGetTypeID()) return -2;
	return CFBooleanGetValue((CFBooleanRef)value) ? 1 : 0;
}

static int caffeinateReadSession(int *onConsole, int *loginDone, int *locked) {
	CFDictionaryRef session = CGSessionCopyCurrentDictionary();
	if (session == NULL) return 0;
	*onConsole = caffeinateSessionFlag(session, kCGSessionOnConsoleKey);
	*loginDone = caffeinateSessionFlag(session, kCGSessionLoginDoneKey);
	// This WindowServer key and the distributed lock notification names are
	// undocumented. Do not substitute screensaver or display-power state.
	*locked = caffeinateSessionFlag(session, CFSTR("CGSSessionScreenIsLocked"));
	CFRelease(session);
	return 1;
}

@interface MyStreamDeckCaffeinateSessionObserver : NSObject
@property uintptr_t token;
@end

@implementation MyStreamDeckCaffeinateSessionObserver
- (void)changed:(NSNotification *)notification {
	int event = 0;
	if ([notification.name isEqualToString:@"com.apple.screenIsLocked"]) event = 1;
	if ([notification.name isEqualToString:@"com.apple.screenIsUnlocked"]) event = 2;
	myStreamDeckCaffeinateSessionChanged(self.token, event);
}
@end

static void *caffeinateObserveSession(uintptr_t token) {
	@autoreleasepool {
		MyStreamDeckCaffeinateSessionObserver *observer = [MyStreamDeckCaffeinateSessionObserver new];
		observer.token = token;
		NSDistributedNotificationCenter *distributed = NSDistributedNotificationCenter.defaultCenter;
		for (NSString *name in @[@"com.apple.screenIsLocked", @"com.apple.screenIsUnlocked"]) {
			[distributed addObserver:observer selector:@selector(changed:) name:name object:nil
				suspensionBehavior:NSNotificationSuspensionBehaviorDeliverImmediately];
		}
		NSNotificationCenter *workspace = NSWorkspace.sharedWorkspace.notificationCenter;
		for (NSString *name in @[NSWorkspaceDidWakeNotification,
			NSWorkspaceSessionDidBecomeActiveNotification, NSWorkspaceSessionDidResignActiveNotification]) {
			[workspace addObserver:observer selector:@selector(changed:) name:name object:nil];
		}
		return (void *)CFBridgingRetain(observer);
	}
}

static void caffeinateStopObservingSession(void *handle) {
	@autoreleasepool {
		MyStreamDeckCaffeinateSessionObserver *observer = CFBridgingRelease(handle);
		[NSDistributedNotificationCenter.defaultCenter removeObserver:observer];
		[NSWorkspace.sharedWorkspace.notificationCenter removeObserver:observer];
	}
}
*/
import "C"

import "sync"

type systemCaffeinateLockMonitor struct{}

func (systemCaffeinateLockMonitor) Locked() (bool, error) {
	var onConsole, loginDone, locked C.int
	available := C.caffeinateReadSession(&onConsole, &loginDone, &locked)
	return caffeinateSessionLocked(available != 0, int(onConsole), int(loginDone), int(locked))
}

var caffeinateSessionObservers = struct {
	sync.Mutex
	next     uintptr
	handlers map[uintptr]func(caffeinateLockEvent)
}{handlers: make(map[uintptr]func(caffeinateLockEvent))}

func (systemCaffeinateLockMonitor) Observe(fn func(caffeinateLockEvent)) (func(), error) {
	caffeinateSessionObservers.Lock()
	caffeinateSessionObservers.next++
	token := caffeinateSessionObservers.next
	caffeinateSessionObservers.handlers[token] = fn
	caffeinateSessionObservers.Unlock()
	handle := C.caffeinateObserveSession(C.uintptr_t(token))
	var once sync.Once
	return func() {
		once.Do(func() {
			caffeinateSessionObservers.Lock()
			delete(caffeinateSessionObservers.handlers, token)
			caffeinateSessionObservers.Unlock()
			C.caffeinateStopObservingSession(handle)
		})
	}, nil
}
