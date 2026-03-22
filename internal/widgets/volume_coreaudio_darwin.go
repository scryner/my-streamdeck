package widgets

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework CoreFoundation

#include <AudioToolbox/AudioHardwareService.h>
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static AudioObjectPropertyAddress myStreamDeckOutputVolumeAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckOutputMuteAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioDevicePropertyMute,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckDefaultOutputDeviceAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckDeviceNameAddress(void) {
	AudioObjectPropertyAddress address = {
		kAudioObjectPropertyName,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static OSStatus myStreamDeckDefaultOutputDevice(AudioObjectID *deviceID) {
	if (deviceID == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	UInt32 size = sizeof(AudioObjectID);
	AudioObjectPropertyAddress address = myStreamDeckDefaultOutputDeviceAddress();
	return AudioObjectGetPropertyData(kAudioObjectSystemObject, &address, 0, NULL, &size, deviceID);
}

static int myStreamDeckObjectHasProperty(AudioObjectID objectID, AudioObjectPropertyAddress address) {
	return AudioObjectHasProperty(objectID, &address) ? 1 : 0;
}

static OSStatus myStreamDeckGetOutputVolume(AudioObjectID deviceID, Float32 *value) {
	if (value == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	AudioObjectPropertyAddress address = myStreamDeckOutputVolumeAddress();
	UInt32 size = sizeof(Float32);
	return AudioObjectGetPropertyData(deviceID, &address, 0, NULL, &size, value);
}

static OSStatus myStreamDeckSetOutputVolume(AudioObjectID deviceID, Float32 value) {
	AudioObjectPropertyAddress address = myStreamDeckOutputVolumeAddress();
	UInt32 size = sizeof(Float32);
	return AudioObjectSetPropertyData(deviceID, &address, 0, NULL, size, &value);
}

static OSStatus myStreamDeckGetOutputMute(AudioObjectID deviceID, UInt32 *value) {
	if (value == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	AudioObjectPropertyAddress address = myStreamDeckOutputMuteAddress();
	UInt32 size = sizeof(UInt32);
	return AudioObjectGetPropertyData(deviceID, &address, 0, NULL, &size, value);
}

static OSStatus myStreamDeckSetOutputMute(AudioObjectID deviceID, UInt32 value) {
	AudioObjectPropertyAddress address = myStreamDeckOutputMuteAddress();
	UInt32 size = sizeof(UInt32);
	return AudioObjectSetPropertyData(deviceID, &address, 0, NULL, size, &value);
}

static char *myStreamDeckCopyOutputDeviceName(AudioObjectID deviceID) {
	CFStringRef name = NULL;
	UInt32 size = sizeof(CFStringRef);
	AudioObjectPropertyAddress address = myStreamDeckDeviceNameAddress();
	OSStatus status = AudioObjectGetPropertyData(deviceID, &address, 0, NULL, &size, &name);
	if (status != noErr || name == NULL) {
		return NULL;
	}

	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(CFStringGetLength(name), kCFStringEncodingUTF8) + 1;
	char *buffer = (char *)malloc((size_t)maxSize);
	if (buffer == NULL) {
		CFRelease(name);
		return NULL;
	}
	if (!CFStringGetCString(name, buffer, maxSize, kCFStringEncodingUTF8)) {
		free(buffer);
		buffer = NULL;
	}
	CFRelease(name);
	return buffer;
}
*/
import "C"

import (
	"context"
	"fmt"
	"math"
	"unsafe"
)

func readVolumeState(context.Context) (VolumeState, error) {
	deviceID, err := defaultOutputDeviceID()
	if err != nil {
		return VolumeState{}, err
	}

	var rawVolume C.Float32
	if status := C.myStreamDeckGetOutputVolume(deviceID, &rawVolume); status != C.noErr {
		return VolumeState{}, fmt.Errorf("read output volume: status=%d", int(status))
	}

	muted := false
	muteAddress := C.myStreamDeckOutputMuteAddress()
	if C.myStreamDeckObjectHasProperty(deviceID, muteAddress) == 1 {
		var rawMute C.UInt32
		if status := C.myStreamDeckGetOutputMute(deviceID, &rawMute); status == C.noErr {
			muted = rawMute != 0
		}
	}

	return VolumeState{
		Volume: clampVolumePercent(int(math.Round(float64(rawVolume) * 100))),
		Muted:  muted,
	}, nil
}

func setOutputVolume(_ context.Context, percent int) error {
	deviceID, err := defaultOutputDeviceID()
	if err != nil {
		return err
	}

	value := C.Float32(float32(clampVolumePercent(percent)) / 100.0)
	if status := C.myStreamDeckSetOutputVolume(deviceID, value); status != C.noErr {
		return fmt.Errorf("set output volume: status=%d", int(status))
	}
	return nil
}

func setOutputMuted(_ context.Context, muted bool) error {
	deviceID, err := defaultOutputDeviceID()
	if err != nil {
		return err
	}

	muteAddress := C.myStreamDeckOutputMuteAddress()
	if C.myStreamDeckObjectHasProperty(deviceID, muteAddress) != 1 {
		return fmt.Errorf("set output mute: device has no mute control")
	}

	value := C.UInt32(0)
	if muted {
		value = 1
	}
	if status := C.myStreamDeckSetOutputMute(deviceID, value); status != C.noErr {
		return fmt.Errorf("set output mute: status=%d", int(status))
	}
	return nil
}

func readOutputSourceName(context.Context) (string, error) {
	deviceID, err := defaultOutputDeviceID()
	if err != nil {
		return "", err
	}

	name := C.myStreamDeckCopyOutputDeviceName(deviceID)
	if name == nil {
		return "", fmt.Errorf("read output source: device name unavailable")
	}
	defer C.free(unsafe.Pointer(name))

	return C.GoString(name), nil
}

func defaultOutputDeviceID() (C.AudioObjectID, error) {
	var deviceID C.AudioObjectID
	if status := C.myStreamDeckDefaultOutputDevice(&deviceID); status != C.noErr {
		return 0, fmt.Errorf("default output device: status=%d", int(status))
	}
	if deviceID == C.kAudioObjectUnknown {
		return 0, fmt.Errorf("default output device: unavailable")
	}
	return deviceID, nil
}
