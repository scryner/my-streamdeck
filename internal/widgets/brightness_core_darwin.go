package widgets

/*
#cgo darwin CFLAGS: -DDARWIN -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework CoreGraphics -framework IOKit -framework CoreFoundation

#include <CoreGraphics/CGDirectDisplay.h>
#include <CoreGraphics/CGDisplayConfiguration.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/graphics/IOGraphicsLib.h>
#include <IOKit/graphics/IOGraphicsTypes.h>
#include <dlfcn.h>
#include <stdlib.h>

typedef int (*MyStreamDeckDisplayServicesCanChangeBrightnessFn)(CGDirectDisplayID display);
typedef int (*MyStreamDeckDisplayServicesGetBrightnessFn)(CGDirectDisplayID display, float *brightness);
typedef int (*MyStreamDeckDisplayServicesSetBrightnessFn)(CGDirectDisplayID display, float brightness);

typedef struct {
	void *handle;
	MyStreamDeckDisplayServicesCanChangeBrightnessFn canChange;
	MyStreamDeckDisplayServicesGetBrightnessFn getBrightness;
	MyStreamDeckDisplayServicesSetBrightnessFn setBrightness;
} MyStreamDeckDisplayServices;

static MyStreamDeckDisplayServices *myStreamDeckDisplayServices(void) {
	static MyStreamDeckDisplayServices services = {0};
	static int attempted = 0;
	if (attempted) {
		return services.handle == NULL ? NULL : &services;
	}
	attempted = 1;

	services.handle = dlopen("/System/Library/PrivateFrameworks/DisplayServices.framework/Versions/A/DisplayServices", RTLD_LAZY);
	if (services.handle == NULL) {
		return NULL;
	}

	services.canChange = (MyStreamDeckDisplayServicesCanChangeBrightnessFn)dlsym(services.handle, "DisplayServicesCanChangeBrightness");
	services.getBrightness = (MyStreamDeckDisplayServicesGetBrightnessFn)dlsym(services.handle, "DisplayServicesGetBrightness");
	services.setBrightness = (MyStreamDeckDisplayServicesSetBrightnessFn)dlsym(services.handle, "DisplayServicesSetBrightness");
	if (services.canChange == NULL || services.getBrightness == NULL || services.setBrightness == NULL) {
		dlclose(services.handle);
		services.handle = NULL;
		services.canChange = NULL;
		services.getBrightness = NULL;
		services.setBrightness = NULL;
		return NULL;
	}

	return &services;
}

static int myStreamDeckCanChangeMainDisplayBrightness(void) {
	MyStreamDeckDisplayServices *services = myStreamDeckDisplayServices();
	CGDirectDisplayID displayID = CGMainDisplayID();
	if (services == NULL || displayID == kCGNullDirectDisplay) {
		return 0;
	}
	return services->canChange(displayID);
}

static io_service_t myStreamDeckMainDisplayService(void) {
	CGDirectDisplayID displayID = CGMainDisplayID();
	if (displayID == kCGNullDirectDisplay) {
		return IO_OBJECT_NULL;
	}
	return CGDisplayIOServicePort(displayID);
}

static int myStreamDeckGetMainDisplayBrightness(float *value) {
	MyStreamDeckDisplayServices *services = myStreamDeckDisplayServices();
	CGDirectDisplayID displayID = CGMainDisplayID();
	if (services == NULL || displayID == kCGNullDirectDisplay) {
		return -1;
	}
	if (!services->canChange(displayID)) {
		return -2;
	}
	return services->getBrightness(displayID, value);
}

static int myStreamDeckSetMainDisplayBrightness(float value) {
	MyStreamDeckDisplayServices *services = myStreamDeckDisplayServices();
	CGDirectDisplayID displayID = CGMainDisplayID();
	if (services == NULL || displayID == kCGNullDirectDisplay) {
		return -1;
	}
	if (!services->canChange(displayID)) {
		return -2;
	}
	return services->setBrightness(displayID, value);
}

static char *myStreamDeckCopyMainDisplayName(void) {
	io_service_t service = myStreamDeckMainDisplayService();
	if (service == IO_OBJECT_NULL) {
		return NULL;
	}

	CFDictionaryRef info = IODisplayCreateInfoDictionary(service, kIODisplayOnlyPreferredName);
	if (info == NULL) {
		return NULL;
	}

	CFDictionaryRef names = (CFDictionaryRef)CFDictionaryGetValue(info, CFSTR(kDisplayProductName));
	if (names == NULL || CFGetTypeID(names) != CFDictionaryGetTypeID()) {
		CFRelease(info);
		return NULL;
	}

	CFIndex count = CFDictionaryGetCount(names);
	if (count <= 0) {
		CFRelease(info);
		return NULL;
	}

	const void **keys = (const void **)malloc((size_t)count * sizeof(void *));
	const void **values = (const void **)malloc((size_t)count * sizeof(void *));
	if (keys == NULL || values == NULL) {
		free(keys);
		free(values);
		CFRelease(info);
		return NULL;
	}

	CFDictionaryGetKeysAndValues(names, keys, values);
	CFStringRef chosen = NULL;
	for (CFIndex i = 0; i < count; i++) {
		if (values[i] != NULL && CFGetTypeID(values[i]) == CFStringGetTypeID()) {
			chosen = (CFStringRef)values[i];
			break;
		}
	}
	free(keys);
	free(values);
	if (chosen == NULL) {
		CFRelease(info);
		return NULL;
	}

	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(CFStringGetLength(chosen), kCFStringEncodingUTF8) + 1;
	char *buffer = (char *)malloc((size_t)maxSize);
	if (buffer == NULL) {
		CFRelease(info);
		return NULL;
	}
	if (!CFStringGetCString(chosen, buffer, maxSize, kCFStringEncodingUTF8)) {
		free(buffer);
		buffer = NULL;
	}
	CFRelease(info);
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

func readMainDisplayBrightness(context.Context) (int, error) {
	var raw C.float
	if status := int(C.myStreamDeckGetMainDisplayBrightness(&raw)); status != 0 {
		return 0, fmt.Errorf("read display brightness: status=%d", status)
	}
	return clampBrightnessPercent(int(math.Round(float64(raw) * 100))), nil
}

func setMainDisplayBrightness(_ context.Context, percent int) error {
	value := C.float(float32(clampBrightnessPercent(percent)) / 100.0)
	if status := int(C.myStreamDeckSetMainDisplayBrightness(value)); status != 0 {
		return fmt.Errorf("set display brightness: status=%d", status)
	}
	return nil
}

func readMainDisplayName(context.Context) (string, error) {
	name := C.myStreamDeckCopyMainDisplayName()
	if name == nil {
		return "", fmt.Errorf("read display name: unavailable")
	}
	defer C.free(unsafe.Pointer(name))
	return C.GoString(name), nil
}
