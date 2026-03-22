package widgets

/*
#cgo darwin CFLAGS: -DDARWIN -x objective-c -fobjc-arc -fblocks
#cgo darwin LDFLAGS: -framework Foundation -framework AppKit -framework ApplicationServices

#include <Foundation/Foundation.h>
#include <AppKit/AppKit.h>
#include <ApplicationServices/ApplicationServices.h>
#include <IOKit/hidsystem/ev_keymap.h>
#include <dispatch/dispatch.h>
#include <dlfcn.h>
#include <stdlib.h>

typedef void (^MyStreamDeckMRPlaybackStateBlock)(unsigned int playbackState);
typedef void (*MyStreamDeckMRGetPlaybackStateFn)(dispatch_queue_t queue, MyStreamDeckMRPlaybackStateBlock block);

typedef struct {
	void *handle;
	MyStreamDeckMRGetPlaybackStateFn getPlaybackState;
} MyStreamDeckMediaRemoteState;

static MyStreamDeckMediaRemoteState *myStreamDeckMediaRemoteState(void) {
	static MyStreamDeckMediaRemoteState mediaRemote = {0};
	static int attempted = 0;
	if (attempted) {
		return mediaRemote.handle == NULL ? NULL : &mediaRemote;
	}
	attempted = 1;

	mediaRemote.handle = dlopen("/System/Library/PrivateFrameworks/MediaRemote.framework/Versions/A/MediaRemote", RTLD_LAZY);
	if (mediaRemote.handle == NULL) {
		return NULL;
	}

	mediaRemote.getPlaybackState = (MyStreamDeckMRGetPlaybackStateFn)dlsym(mediaRemote.handle, "MRMediaRemoteGetNowPlayingApplicationPlaybackState");
	if (mediaRemote.getPlaybackState == NULL) {
		dlclose(mediaRemote.handle);
		mediaRemote.handle = NULL;
		return NULL;
	}

	return &mediaRemote;
}

static int myStreamDeckReadPlaybackState(unsigned int *state) {
	MyStreamDeckMediaRemoteState *mediaRemote = myStreamDeckMediaRemoteState();
	if (state == NULL) {
		return -3;
	}
	*state = 0;

	if (mediaRemote == NULL || mediaRemote->getPlaybackState == NULL) {
		return -1;
	}

	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	dispatch_queue_t queue = dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0);

	mediaRemote->getPlaybackState(queue, ^(unsigned int playbackState) {
		*state = playbackState;
		dispatch_semaphore_signal(sem);
	});

	dispatch_time_t timeout = dispatch_time(DISPATCH_TIME_NOW, 2LL * NSEC_PER_SEC);
	if (dispatch_semaphore_wait(sem, timeout) != 0) {
		return -2;
	}

	return 0;
}

static void myStreamDeckPostMediaKey(int keyType) {
	@autoreleasepool {
		NSEvent *down = [NSEvent otherEventWithType:NSEventTypeSystemDefined
		                                   location:NSZeroPoint
		                              modifierFlags:0xA00
		                                  timestamp:0
		                               windowNumber:0
		                                    context:nil
		                                    subtype:8
		                                      data1:(keyType << 16) | (0xA << 8)
		                                      data2:-1];
		CGEventPost(kCGHIDEventTap, [down CGEvent]);

		NSEvent *up = [NSEvent otherEventWithType:NSEventTypeSystemDefined
		                                 location:NSZeroPoint
		                            modifierFlags:0xB00
		                                timestamp:0
		                             windowNumber:0
		                                  context:nil
		                                  subtype:8
		                                    data1:(keyType << 16) | (0xB << 8)
		                                    data2:-1];
		CGEventPost(kCGHIDEventTap, [up CGEvent]);
	}
}
*/
import "C"

import "context"

func readSystemPlaybackState(context.Context) (playerPlaybackState, error) {
	var raw C.uint
	if status := int(C.myStreamDeckReadPlaybackState(&raw)); status != 0 {
		return playerPlaybackStateUnknown, nil
	}

	switch int(raw) {
	case 1:
		return playerPlaybackStatePlaying, nil
	case 2, 3, 4:
		return playerPlaybackStatePaused, nil
	default:
		return playerPlaybackStateUnknown, nil
	}
}

func sendSystemPlayPause(context.Context) error {
	C.myStreamDeckPostMediaKey(C.NX_KEYTYPE_PLAY)
	return nil
}

func sendSystemNextTrack(context.Context) error {
	C.myStreamDeckPostMediaKey(C.NX_KEYTYPE_NEXT)
	return nil
}

func sendSystemPreviousTrack(context.Context) error {
	C.myStreamDeckPostMediaKey(C.NX_KEYTYPE_PREVIOUS)
	return nil
}
