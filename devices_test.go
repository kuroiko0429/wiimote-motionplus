package main

import (
	"fmt"
	"testing"
	"time"
)

func node(name, parent, uniq, event string) string {
	return fmt.Sprintf("N: Name=\"%s\"\nP: Phys=\nS: Sysfs=%s/input/input%s\nU: Uniq=%s\nH: Handlers=%s\n\n", name, parent, event[5:], uniq, event)
}
func TestDiscoveryGroupsEmptyIdentifiersByHIDParent(t *testing.T) {
	text := node("Nintendo Wii Remote", "/devices/hidA", "", "event1") + node("Nintendo Wii Remote Accelerometer", "/devices/hidB", "", "event2") + node("Nintendo Wii Remote Motion Plus", "/devices/hidB", "", "event3")
	if _, err := selectDevices(parseDevices(text), "gyro"); err == nil {
		t.Fatal("mixed two remotes")
	}
	text += node("Nintendo Wii Remote", "/devices/hidB", "", "event4")
	d, err := selectDevices(parseDevices(text), "auto")
	if err != nil || d.motion != "/dev/input/event3" || d.button != "/dev/input/event4" || d.mode != "gyro" {
		t.Fatalf("%+v %v", d, err)
	}
}
func TestEnumerationWaitsForGyroBeforeAutoFallback(t *testing.T) {
	now := time.Unix(1, 0)
	calls := 0
	scan := func() ([]deviceSet, error) {
		calls++
		text := node("Nintendo Wii Remote", "/devices/hidA", "", "event1") + node("Nintendo Wii Remote Accelerometer", "/devices/hidA", "", "event2")
		if calls >= 3 {
			text += node("Nintendo Wii Remote Motion Plus", "/devices/hidA", "", "event3")
		}
		return parseDevices(text), nil
	}
	d, err := waitDevices("auto", time.Second, scan, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
	if err != nil || d.mode != "gyro" || calls != 3 {
		t.Fatalf("%+v %v calls=%d", d, err, calls)
	}
}
