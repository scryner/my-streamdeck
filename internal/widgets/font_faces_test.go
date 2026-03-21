package widgets

import (
	"reflect"
	"testing"

	"golang.org/x/image/font"
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
