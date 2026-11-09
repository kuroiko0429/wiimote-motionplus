package main

import (
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"
)

func TestOptionsRejectInvalidBeforeHardware(t *testing.T) {
	for _, args := range [][]string{{"--mode=other"}, {"--gyro-sensitivity=NaN"}, {"--gyro-sensitivity=+Inf"}, {"--gyro-sensitivity=0"}, {"--gyro-deadzone=-1"}, {"--gyro-deadzone=NaN"}, {"stray"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := parseOptions(nil, io.Discard)
	if err != nil || o.mode != "auto" || o.gyroSensitivity != 10 {
		t.Fatalf("%+v %v", o, err)
	}
}
func TestReadLoopPreservesPartialRecordsAndReportsEOF(t *testing.T) {
	rd, wr := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan taggedEvent, 8)
	go readEvents(ctx, rd, srcAccel, events)
	b := make([]byte, 24)
	binary.NativeEndian.PutUint16(b[16:], evAbs)
	binary.NativeEndian.PutUint16(b[18:], absRX)
	binary.NativeEndian.PutUint32(b[20:], 123)
	go func() { wr.Write(b[:7]); wr.Write(b[7:]); wr.Close() }()
	select {
	case e := <-events:
		if e.ev.value != 123 || e.err != nil {
			t.Fatalf("%+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
	select {
	case e := <-events:
		if e.err == nil {
			t.Fatal("EOF not signaled")
		}
	case <-time.After(time.Second):
		t.Fatal("missing disconnect")
	}
}
func TestRuntimeDroppedEventsFailClosed(t *testing.T) {
	r := newRuntime("gyro", 10, 0, time.Unix(1, 0), func(rawEvent) { t.Fatal("output after drop") })
	if err := r.event(taggedEvent{src: srcAccel, ev: rawEvent{evSyn, 3, 0}}, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "SYN_DROPPED") {
		t.Fatalf("%v", err)
	}
}
