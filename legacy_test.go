package main

import (
	"testing"
	"time"
)

func TestLegacyHeldTiltStillMovesWithoutChangedAxisFrames(t *testing.T) {
	n := time.Unix(10, 0)
	var got []rawEvent
	r := newRuntime("accel", 10, 0, n, func(e rawEvent) { got = append(got, e) })
	r.centered = true
	r.axes = [2]int32{10, 0}
	r.lastFrame = n
	r.move(n.Add(time.Second))
	if len(got) != 3 || got[0].value != 3 {
		t.Fatalf("held legacy tilt stopped: %+v", got)
	}
}
