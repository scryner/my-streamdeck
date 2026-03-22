package widgets

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreAudio -framework AudioToolbox

#include <AudioToolbox/AudioHardwareService.h>
#include <CoreAudio/CoreAudio.h>
#include <stdint.h>
#include <stdlib.h>

extern void myStreamDeckHandleVolumeChange(uintptr_t token);

typedef struct {
	uintptr_t token;
	AudioObjectID currentDeviceID;
	int hasDefaultOutputListener;
	int hasServiceRestartListener;
	int hasVolumeListener;
	int hasMuteListener;
	int hasDataSourceListener;
} MyStreamDeckVolumeObserver;

static AudioObjectPropertyAddress myStreamDeckDefaultOutputAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckServiceRestartAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioHardwareServiceProperty_ServiceRestarted,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckVolumeAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckMuteAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioDevicePropertyMute,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckDataSourceAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioDevicePropertyDataSource,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static void myStreamDeckVolumeObserverUnregisterCurrentDeviceListeners(MyStreamDeckVolumeObserver *observer);
static void myStreamDeckVolumeObserverRegisterCurrentDeviceListeners(MyStreamDeckVolumeObserver *observer);

static OSStatus myStreamDeckVolumePropertyListener(AudioObjectID inObjectID,
	UInt32 inNumberAddresses,
	const AudioObjectPropertyAddress inAddresses[],
	void *inClientData) {
	(void)inObjectID;
	MyStreamDeckVolumeObserver *observer = (MyStreamDeckVolumeObserver *)inClientData;
	if (observer == NULL) {
		return noErr;
	}

	int shouldRebind = 0;
	for (UInt32 i = 0; i < inNumberAddresses; i++) {
		AudioObjectPropertySelector selector = inAddresses[i].mSelector;
		if (selector == kAudioHardwarePropertyDefaultOutputDevice || selector == kAudioHardwareServiceProperty_ServiceRestarted) {
			shouldRebind = 1;
			break;
		}
	}

	if (shouldRebind) {
		myStreamDeckVolumeObserverUnregisterCurrentDeviceListeners(observer);
		myStreamDeckVolumeObserverRegisterCurrentDeviceListeners(observer);
	}

	myStreamDeckHandleVolumeChange(observer->token);
	return noErr;
}

static void myStreamDeckVolumeObserverUnregisterCurrentDeviceListeners(MyStreamDeckVolumeObserver *observer) {
	if (observer == NULL || observer->currentDeviceID == kAudioObjectUnknown) {
		return;
	}

	AudioObjectPropertyAddress volumeAddress = myStreamDeckVolumeAddress();
	AudioObjectPropertyAddress muteAddress = myStreamDeckMuteAddress();
	AudioObjectPropertyAddress dataSourceAddress = myStreamDeckDataSourceAddress();

	if (observer->hasVolumeListener) {
		AudioObjectRemovePropertyListener(observer->currentDeviceID, &volumeAddress, myStreamDeckVolumePropertyListener, observer);
		observer->hasVolumeListener = 0;
	}
	if (observer->hasMuteListener) {
		AudioObjectRemovePropertyListener(observer->currentDeviceID, &muteAddress, myStreamDeckVolumePropertyListener, observer);
		observer->hasMuteListener = 0;
	}
	if (observer->hasDataSourceListener) {
		AudioObjectRemovePropertyListener(observer->currentDeviceID, &dataSourceAddress, myStreamDeckVolumePropertyListener, observer);
		observer->hasDataSourceListener = 0;
	}

	observer->currentDeviceID = kAudioObjectUnknown;
}

static void myStreamDeckVolumeObserverRegisterCurrentDeviceListeners(MyStreamDeckVolumeObserver *observer) {
	if (observer == NULL) {
		return;
	}

	AudioObjectID deviceID = kAudioObjectUnknown;
	UInt32 size = sizeof(deviceID);
	AudioObjectPropertyAddress defaultOutputAddress = myStreamDeckDefaultOutputAddress();
	OSStatus status = AudioObjectGetPropertyData(kAudioObjectSystemObject, &defaultOutputAddress, 0, NULL, &size, &deviceID);
	if (status != noErr || deviceID == kAudioObjectUnknown) {
		return;
	}

	observer->currentDeviceID = deviceID;

	AudioObjectPropertyAddress volumeAddress = myStreamDeckVolumeAddress();
	AudioObjectPropertyAddress muteAddress = myStreamDeckMuteAddress();
	AudioObjectPropertyAddress dataSourceAddress = myStreamDeckDataSourceAddress();

	if (AudioObjectAddPropertyListener(deviceID, &volumeAddress, myStreamDeckVolumePropertyListener, observer) == noErr) {
		observer->hasVolumeListener = 1;
	}
	if (AudioObjectAddPropertyListener(deviceID, &muteAddress, myStreamDeckVolumePropertyListener, observer) == noErr) {
		observer->hasMuteListener = 1;
	}
	if (AudioObjectAddPropertyListener(deviceID, &dataSourceAddress, myStreamDeckVolumePropertyListener, observer) == noErr) {
		observer->hasDataSourceListener = 1;
	}
}

static MyStreamDeckVolumeObserver *myStreamDeckStartVolumeObserver(uintptr_t token) {
	MyStreamDeckVolumeObserver *observer = (MyStreamDeckVolumeObserver *)calloc(1, sizeof(MyStreamDeckVolumeObserver));
	if (observer == NULL) {
		return NULL;
	}

	observer->token = token;
	observer->currentDeviceID = kAudioObjectUnknown;

	AudioObjectPropertyAddress defaultOutputAddress = myStreamDeckDefaultOutputAddress();
	if (AudioObjectAddPropertyListener(kAudioObjectSystemObject, &defaultOutputAddress, myStreamDeckVolumePropertyListener, observer) == noErr) {
		observer->hasDefaultOutputListener = 1;
	}

	AudioObjectPropertyAddress serviceRestartAddress = myStreamDeckServiceRestartAddress();
	if (AudioObjectAddPropertyListener(kAudioObjectSystemObject, &serviceRestartAddress, myStreamDeckVolumePropertyListener, observer) == noErr) {
		observer->hasServiceRestartListener = 1;
	}

	if (!observer->hasDefaultOutputListener && !observer->hasServiceRestartListener) {
		free(observer);
		return NULL;
	}

	myStreamDeckVolumeObserverRegisterCurrentDeviceListeners(observer);
	return observer;
}

static void myStreamDeckStopVolumeObserver(MyStreamDeckVolumeObserver *observer) {
	if (observer == NULL) {
		return;
	}

	myStreamDeckVolumeObserverUnregisterCurrentDeviceListeners(observer);

	AudioObjectPropertyAddress defaultOutputAddress = myStreamDeckDefaultOutputAddress();
	AudioObjectPropertyAddress serviceRestartAddress = myStreamDeckServiceRestartAddress();

	if (observer->hasDefaultOutputListener) {
		AudioObjectRemovePropertyListener(kAudioObjectSystemObject, &defaultOutputAddress, myStreamDeckVolumePropertyListener, observer);
		observer->hasDefaultOutputListener = 0;
	}
	if (observer->hasServiceRestartListener) {
		AudioObjectRemovePropertyListener(kAudioObjectSystemObject, &serviceRestartAddress, myStreamDeckVolumePropertyListener, observer);
		observer->hasServiceRestartListener = 0;
	}

	free(observer);
}
*/
import "C"

import (
	"fmt"
	"sync"
)

var (
	volumeObserverMu       sync.Mutex
	volumeObserverHandlers         = map[uintptr]func(){}
	nextVolumeObserverID   uintptr = 1
)

func startVolumeObserver(fn func()) (func(), error) {
	if fn == nil {
		return func() {}, nil
	}

	volumeObserverMu.Lock()
	token := nextVolumeObserverID
	nextVolumeObserverID++
	volumeObserverHandlers[token] = fn
	volumeObserverMu.Unlock()

	observer := C.myStreamDeckStartVolumeObserver(C.uintptr_t(token))
	if observer == nil {
		volumeObserverMu.Lock()
		delete(volumeObserverHandlers, token)
		volumeObserverMu.Unlock()
		return nil, fmt.Errorf("start volume observer: register Core Audio property listeners")
	}

	return func() {
		C.myStreamDeckStopVolumeObserver(observer)
		volumeObserverMu.Lock()
		delete(volumeObserverHandlers, token)
		volumeObserverMu.Unlock()
	}, nil
}

//export myStreamDeckHandleVolumeChange
func myStreamDeckHandleVolumeChange(token C.uintptr_t) {
	volumeObserverMu.Lock()
	handler := volumeObserverHandlers[uintptr(token)]
	volumeObserverMu.Unlock()
	if handler != nil {
		handler()
	}
}
