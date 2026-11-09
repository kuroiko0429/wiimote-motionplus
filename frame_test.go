package main

import (
	"testing"
	"time"
)

func TestCalibrationTickDoesNotObservePartialFrame(t *testing.T) {
	n := time.Unix(10, 0)
	r := newRuntime("gyro", 10, 0, n, func(rawEvent) {})
	r.event(taggedEvent{src: srcAccel, ev: rawEvent{evSyn, 0, 0}}, n)
	r.event(taggedEvent{src: srcAccel, ev: rawEvent{evAbs, absRX, 10000}}, n.Add(time.Millisecond))
	r.move(n.Add(2 * time.Millisecond))
	if r.gyro.sum[0] != 0 {
		t.Fatalf("uncommitted yaw entered calibration: %v", r.gyro.sum)
	}
}
