package widgets

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework CoreFoundation

#include <AudioToolbox/AudioHardwareService.h>
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

#define MY_STREAM_DECK_ENDPOINT_OUTPUT 0
#define MY_STREAM_DECK_ENDPOINT_INPUT 1

static AudioObjectPropertyScope myStreamDeckEndpointScope(int kind) {
	return kind == MY_STREAM_DECK_ENDPOINT_INPUT ? kAudioDevicePropertyScopeInput : kAudioDevicePropertyScopeOutput;
}

static AudioObjectPropertySelector myStreamDeckDefaultDeviceSelector(int kind) {
	return kind == MY_STREAM_DECK_ENDPOINT_INPUT ? kAudioHardwarePropertyDefaultInputDevice : kAudioHardwarePropertyDefaultOutputDevice;
}

static AudioObjectPropertyAddress myStreamDeckEndpointVolumeAddress(int kind) {
	AudioObjectPropertyAddress address = {
		kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
		myStreamDeckEndpointScope(kind),
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckEndpointMuteAddress(int kind) {
	AudioObjectPropertyAddress address = {
		kAudioDevicePropertyMute,
		myStreamDeckEndpointScope(kind),
		kAudioObjectPropertyElementMain,
	};
	return address;
}

static AudioObjectPropertyAddress myStreamDeckDefaultDeviceAddress(int kind) {
	AudioObjectPropertyAddress address = {
		myStreamDeckDefaultDeviceSelector(kind),
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

static OSStatus myStreamDeckDefaultDevice(int kind, AudioObjectID *deviceID) {
	if (deviceID == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	UInt32 size = sizeof(AudioObjectID);
	AudioObjectPropertyAddress address = myStreamDeckDefaultDeviceAddress(kind);
	return AudioObjectGetPropertyData(kAudioObjectSystemObject, &address, 0, NULL, &size, deviceID);
}

static int myStreamDeckObjectHasProperty(AudioObjectID objectID, AudioObjectPropertyAddress address) {
	return AudioObjectHasProperty(objectID, &address) ? 1 : 0;
}

static OSStatus myStreamDeckGetEndpointVolume(int kind, AudioObjectID deviceID, Float32 *value) {
	if (value == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	AudioObjectPropertyAddress address = myStreamDeckEndpointVolumeAddress(kind);
	UInt32 size = sizeof(Float32);
	return AudioObjectGetPropertyData(deviceID, &address, 0, NULL, &size, value);
}

static OSStatus myStreamDeckSetEndpointVolume(int kind, AudioObjectID deviceID, Float32 value) {
	AudioObjectPropertyAddress address = myStreamDeckEndpointVolumeAddress(kind);
	UInt32 size = sizeof(Float32);
	return AudioObjectSetPropertyData(deviceID, &address, 0, NULL, size, &value);
}

static OSStatus myStreamDeckGetEndpointMute(int kind, AudioObjectID deviceID, UInt32 *value) {
	if (value == NULL) {
		return kAudioHardwareIllegalOperationError;
	}
	AudioObjectPropertyAddress address = myStreamDeckEndpointMuteAddress(kind);
	UInt32 size = sizeof(UInt32);
	return AudioObjectGetPropertyData(deviceID, &address, 0, NULL, &size, value);
}

static OSStatus myStreamDeckSetEndpointMute(int kind, AudioObjectID deviceID, UInt32 value) {
	AudioObjectPropertyAddress address = myStreamDeckEndpointMuteAddress(kind);
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

func readAudioEndpointState(kind audioEndpointKind, _ context.Context) (VolumeState, error) {
	deviceID, err := defaultDeviceID(kind)
	if err != nil {
		return VolumeState{}, err
	}

	var rawVolume C.Float32
	if status := C.myStreamDeckGetEndpointVolume(C.int(kind), deviceID, &rawVolume); status != C.noErr {
		return VolumeState{}, fmt.Errorf("read output volume: status=%d", int(status))
	}

	muted := false
	muteAddress := C.myStreamDeckEndpointMuteAddress(C.int(kind))
	if C.myStreamDeckObjectHasProperty(deviceID, muteAddress) == 1 {
		var rawMute C.UInt32
		if status := C.myStreamDeckGetEndpointMute(C.int(kind), deviceID, &rawMute); status == C.noErr {
			muted = rawMute != 0
		}
	}

	return VolumeState{
		Volume: clampVolumePercent(int(math.Round(float64(rawVolume) * 100))),
		Muted:  muted,
	}, nil
}

func readVolumeState(ctx context.Context) (VolumeState, error) {
	return readAudioEndpointState(audioEndpointOutput, ctx)
}

func readInputVolumeState(ctx context.Context) (VolumeState, error) {
	return readAudioEndpointState(audioEndpointInput, ctx)
}

func setAudioEndpointVolume(kind audioEndpointKind, _ context.Context, percent int) error {
	deviceID, err := defaultDeviceID(kind)
	if err != nil {
		return err
	}

	value := C.Float32(float32(clampVolumePercent(percent)) / 100.0)
	if status := C.myStreamDeckSetEndpointVolume(C.int(kind), deviceID, value); status != C.noErr {
		return fmt.Errorf("set output volume: status=%d", int(status))
	}
	return nil
}

func setOutputVolume(ctx context.Context, percent int) error {
	return setAudioEndpointVolume(audioEndpointOutput, ctx, percent)
}

func setInputVolume(ctx context.Context, percent int) error {
	return setAudioEndpointVolume(audioEndpointInput, ctx, percent)
}

func setAudioEndpointMuted(kind audioEndpointKind, _ context.Context, muted bool) error {
	deviceID, err := defaultDeviceID(kind)
	if err != nil {
		return err
	}

	muteAddress := C.myStreamDeckEndpointMuteAddress(C.int(kind))
	if C.myStreamDeckObjectHasProperty(deviceID, muteAddress) != 1 {
		return fmt.Errorf("set output mute: device has no mute control")
	}

	value := C.UInt32(0)
	if muted {
		value = 1
	}
	if status := C.myStreamDeckSetEndpointMute(C.int(kind), deviceID, value); status != C.noErr {
		return fmt.Errorf("set output mute: status=%d", int(status))
	}
	return nil
}

func setOutputMuted(ctx context.Context, muted bool) error {
	return setAudioEndpointMuted(audioEndpointOutput, ctx, muted)
}

func setInputMuted(ctx context.Context, muted bool) error {
	return setAudioEndpointMuted(audioEndpointInput, ctx, muted)
}

func readAudioEndpointSourceName(kind audioEndpointKind, _ context.Context) (string, error) {
	deviceID, err := defaultDeviceID(kind)
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

func readOutputSourceName(ctx context.Context) (string, error) {
	return readAudioEndpointSourceName(audioEndpointOutput, ctx)
}

func readInputSourceName(ctx context.Context) (string, error) {
	return readAudioEndpointSourceName(audioEndpointInput, ctx)
}

func defaultDeviceID(kind audioEndpointKind) (C.AudioObjectID, error) {
	var deviceID C.AudioObjectID
	if status := C.myStreamDeckDefaultDevice(C.int(kind), &deviceID); status != C.noErr {
		return 0, fmt.Errorf("default output device: status=%d", int(status))
	}
	if deviceID == C.kAudioObjectUnknown {
		return 0, fmt.Errorf("default output device: unavailable")
	}
	return deviceID, nil
}
