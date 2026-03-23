package widgets

import (
	"os"
	"reflect"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
)

func TestLoadFacesReturnDistinctInstances(t *testing.T) {
	t.Parallel()

	calendarA, err := loadCalendarFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadCalendarFaces A: %v", err)
	}
	calendarB, err := loadCalendarFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadCalendarFaces B: %v", err)
	}
	if sameFace(calendarA.header, calendarB.header) || sameFace(calendarA.day, calendarB.day) {
		t.Fatal("expected calendar faces to be unique per load")
	}

	sysstatA, err := loadSysstatFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadSysstatFaces A: %v", err)
	}
	sysstatB, err := loadSysstatFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadSysstatFaces B: %v", err)
	}
	if sameFace(sysstatA.value, sysstatB.value) {
		t.Fatal("expected sysstat faces to be unique per load")
	}

	netstatA, err := loadNetstatFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadNetstatFaces A: %v", err)
	}
	netstatB, err := loadNetstatFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadNetstatFaces B: %v", err)
	}
	if sameFace(netstatA.value, netstatB.value) || sameFace(netstatA.iface, netstatB.iface) {
		t.Fatal("expected netstat faces to be unique per load")
	}

	weatherA, err := loadWeatherFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadWeatherFaces A: %v", err)
	}
	weatherB, err := loadWeatherFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadWeatherFaces B: %v", err)
	}
	if sameFace(weatherA.today, weatherB.today) || sameFace(weatherA.todayTemp, weatherB.todayTemp) {
		t.Fatal("expected weather faces to be unique per load")
	}

	caffeinateA, err := loadCaffeinateFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadCaffeinateFaces A: %v", err)
	}
	caffeinateB, err := loadCaffeinateFaces(DefaultClockWidgetSize)
	if err != nil {
		t.Fatalf("loadCaffeinateFaces B: %v", err)
	}
	if sameFace(caffeinateA.status, caffeinateB.status) {
		t.Fatal("expected caffeinate faces to be unique per load")
	}
}

func TestNewFaceFromFileCachesParsedFont(t *testing.T) {
	path := t.TempDir() + "/cached-font.ttf"
	if err := os.WriteFile(path, gobold.TTF, 0o600); err != nil {
		t.Fatalf("write temp font: %v", err)
	}

	first, err := newFaceFromFile(path, 16)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	t.Cleanup(func() {
		_ = first.Close()
	})

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temp font: %v", err)
	}

	second, err := newFaceFromFile(path, 16)
	if err != nil {
		t.Fatalf("second load from cache: %v", err)
	}
	t.Cleanup(func() {
		_ = second.Close()
	})

	if sameFace(first, second) {
		t.Fatal("expected cached font loads to return distinct face instances")
	}
}

func sameFace(a, b font.Face) bool {
	if a == nil || b == nil {
		return a == b
	}

	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if !va.IsValid() || !vb.IsValid() {
		return !va.IsValid() && !vb.IsValid()
	}
	if va.Kind() == reflect.Pointer && vb.Kind() == reflect.Pointer {
		return va.Pointer() == vb.Pointer()
	}
	return reflect.DeepEqual(a, b)
}
