package main

import (
	"testing"
	"time"
)

func TestCalibrationCompletesWhenEvdevSuppressesUnchangedStationaryAxes(t *testing.T) {
	n := time.Unix(10, 0)
	r := newRuntime("gyro", 10, 0, n, func(rawEvent) { t.Fatal("stationary calibration moved") })
	if err := r.initializeAxes(func(code uint16) (int32, error) {
		if code == absRX {
			return 100, nil
		}
		return 200, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.event(taggedEvent{src: srcAccel, ev: rawEvent{evSyn, 0, 0}}, n)
	// ABS fuzz/change filtering can suppress all subsequent unchanged frames.
	for i := 0; i <= 100; i++ {
		r.move(n.Add(time.Duration(i) * 10 * time.Millisecond))
	}
	if !r.gyro.ready || r.gyro.bias != [2]float64{100, 200} {
		t.Fatalf("calibration stuck: %+v", r.gyro)
	}
	r.move(n.Add(2 * time.Second))
}
