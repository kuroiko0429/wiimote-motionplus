package main

import (
	"fmt"
	"testing"
	"time"
)

func TestInitialAxisSnapshotCompletesUnchangedEvdevAxes(t *testing.T) {
	n := time.Unix(10, 0)
	r := newRuntime("gyro", 10, 0, n, func(rawEvent) {})
	err := r.initializeAxes(func(code uint16) (int32, error) {
		switch code {
		case absRX:
			return 100, nil
		case 5:
			return 200, nil
		default:
			return 0, fmt.Errorf("must not read roll %d", code)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// evdev suppresses unchanged axes; a frame may contain only roll.
	for i := 0; i <= 100; i++ {
		at := n.Add(time.Duration(i) * 10 * time.Millisecond)
		r.event(taggedEvent{src: srcAccel, ev: rawEvent{evAbs, absRY, 999}}, at)
		r.event(taggedEvent{src: srcAccel, ev: rawEvent{evSyn, 0, 0}}, at)
	}
	if !r.gyro.ready || r.gyro.bias != [2]float64{100, 200} {
		t.Fatalf("bias=%v ready=%v", r.gyro.bias, r.gyro.ready)
	}
}
