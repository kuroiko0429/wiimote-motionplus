package main

import (
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"
)

func TestServeConsumesRealReaderEventsAndExitsOnDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rd, wr := io.Pipe()
	defer rd.Close()
	events := make(chan taggedEvent, 8)
	go readEvents(ctx, rd, srcButton, events)
	go func() {
		defer wr.Close()
		for _, v := range []int32{1, 0} {
			b := make([]byte, 24)
			binary.NativeEndian.PutUint16(b[16:], evKey)
			binary.NativeEndian.PutUint16(b[18:], btnA)
			binary.NativeEndian.PutUint32(b[20:], uint32(v))
			wr.Write(b)
		}
	}()
	var got []rawEvent
	r := newRuntime("gyro", 10, 0, time.Unix(10, 0), func(e rawEvent) { got = append(got, e) })
	err := serve(ctx, r, events, nil, nil, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("%v", err)
	}
	if len(got) != 4 || got[0].code != btnLeft || got[0].value != 1 || got[2].value != 0 {
		t.Fatalf("%+v", got)
	}
}
func TestAutoFallbackAndExplicitGyroTimeout(t *testing.T) {
	text := node("Nintendo Wii Remote", "/devices/A", "", "event1") + node("Nintendo Wii Remote Accelerometer", "/devices/A", "", "event2")
	for _, mode := range []string{"auto", "gyro", "accel"} {
		now := time.Unix(10, 0)
		start := now
		d, err := waitDevices(mode, time.Second, func() ([]deviceSet, error) { return parseDevices(text), nil }, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		if mode == "gyro" {
			if err == nil || !strings.Contains(err.Error(), "MotionPlus") {
				t.Fatalf("%+v %v", d, err)
			}
		} else if err != nil || d.mode != "accel" {
			t.Fatalf("%+v %v", d, err)
		}
		expected := time.Second
		if mode == "accel" {
			expected = 0
		}
		if now.Sub(start) != expected {
			t.Fatalf("%s waited %s", mode, now.Sub(start))
		}
	}
}
func TestLegacyTiltScrollingAndSensitivityMappings(t *testing.T) {
	n := time.Unix(10, 0)
	var got []rawEvent
	r := newRuntime("accel", 10, 0, n, func(e rawEvent) { got = append(got, e) })
	event := func(src source, typ, code uint16, v int32) {
		if err := r.event(taggedEvent{src: src, ev: rawEvent{typ, code, v}}, n); err != nil {
			t.Fatal(err)
		}
	}
	event(srcAccel, evAbs, absRX, 100)
	event(srcAccel, evAbs, absRY, 200)
	event(srcAccel, evSyn, 0, 0)
	r.move(n)
	if len(got) != 0 {
		t.Fatal("initial tilt not centered")
	}
	event(srcAccel, evAbs, absRX, 110)
	event(srcAccel, evSyn, 0, 0)
	r.move(n)
	if len(got) != 3 || got[0].value != 3 {
		t.Fatalf("%+v", got)
	}
	event(srcButton, evKey, btn1, 1)
	got = nil
	r.move(n)
	if len(got) != 0 {
		t.Fatal("legacy recenter failed")
	}
	event(srcButton, evKey, keyNext, 1)
	if r.sensitivity <= sensitivityDefault {
		t.Fatal("plus mapping")
	}
	event(srcButton, evKey, keyPrevious, 1)
	for _, code := range []uint16{keyUp, keyDown, keyLeft, keyRight} {
		event(srcButton, evKey, code, 1)
	}
	r.scrollTick()
	if len(got) != 8 {
		t.Fatalf("scroll %+v", got)
	}
}
