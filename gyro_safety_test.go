package main

import (
	"testing"
	"time"
)

func readyGyro(t *testing.T) *gyro {
	t.Helper()
	start := time.Unix(10, 0)
	g := newGyro(10, 0, start)
	for i := 0; i <= 100; i++ {
		g.sample(0, 0, start.Add(time.Duration(i)*10*time.Millisecond))
	}
	return g
}
func TestGyroStopsForStalePauseAndRecalibration(t *testing.T) {
	g := readyGyro(t)
	n := time.Unix(11, 0).Add(time.Millisecond)
	g.sample(100*gyroUnitsPerDegree, 0, n)
	g.move(n, false)
	g.move(n.Add(50*time.Millisecond), false)
	if x, y := g.move(n.Add(time.Second), false); x != 0 || y != 0 {
		t.Fatal("stale motion")
	}
	g.sample(100*gyroUnitsPerDegree, 0, n.Add(time.Second))
	if x, _ := g.move(n.Add(time.Second+time.Millisecond), true); x != 0 {
		t.Fatal("paused motion")
	}
	g.calibrate(n.Add(2 * time.Second))
	for i := 0; i <= 100; i++ {
		g.sample(0, 0, n.Add(2*time.Second+time.Duration(i)*10*time.Millisecond))
	}
	g.move(n.Add(3*time.Second), false)
	if x, y := g.move(n.Add(3*time.Second+10*time.Millisecond), false); x != 0 || y != 0 {
		t.Fatalf("old rate survived calibration: %d %d", x, y)
	}
}
func TestCalibrationRequiresContinuousFreshSamples(t *testing.T) {
	n := time.Unix(10, 0)
	g := newGyro(10, 0, n)
	for i := 0; i < 19; i++ {
		g.sample(100, 0, n.Add(time.Duration(i)*time.Millisecond))
	}
	g.sample(900, 0, n.Add(2*time.Second))
	if g.ready {
		t.Fatal("calibrated across sample outage")
	}
}
