package main

import (
	"testing"
	"time"
)

func TestRuntimeGyroFramesCalibrationButtonsAndMotion(t *testing.T) {
	n := time.Unix(10, 0)
	var out []rawEvent
	r := newRuntime("gyro", 10, 0, n, func(e rawEvent) { out = append(out, e) })
	frame := func(yaw, roll, pitch int32, at time.Time) {
		for _, e := range []rawEvent{{evAbs, absRX, yaw}, {evAbs, absRY, roll}, {evAbs, 5, pitch}, {evSyn, 0, 0}} {
			if err := r.event(taggedEvent{src: srcAccel, ev: e}, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i <= 100; i++ {
		frame(0, 0, 0, n.Add(time.Duration(i)*10*time.Millisecond))
	}
	n = n.Add(time.Second)
	frame(1676, 100000, 3351, n)
	r.move(n)
	r.move(n.Add(20 * time.Millisecond))
	if len(out) != 3 || out[0].code != relX || out[0].value != -2 || out[1].code != relY || out[1].value != 3 {
		t.Fatalf("yaw/pitch output: %+v", out)
	}
	out = nil
	for _, code := range []uint16{btnA, btnB, btn2} {
		r.event(taggedEvent{src: srcButton, ev: rawEvent{evKey, code, 1}}, n)
	}
	if len(out) != 8 || out[0].code != btnLeft || out[2].code != btnRight || out[4].code != btnMiddle || out[6].value != 0 {
		t.Fatalf("button mapping: %+v", out)
	}
	r.event(taggedEvent{src: srcButton, ev: rawEvent{evKey, btnMode, 1}}, n)
	out = nil
	r.move(n.Add(30 * time.Millisecond))
	if len(out) != 0 {
		t.Fatal("pause moved")
	}
	r.event(taggedEvent{src: srcButton, ev: rawEvent{evKey, btn1, 1}}, n)
	if r.gyro.ready {
		t.Fatal("1 did not restart bias averaging")
	}
}
