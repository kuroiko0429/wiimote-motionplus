package main

import (
	"math"
	"testing"
	"time"
)

func TestGyroCalibratesAverageThenIntegratesYawPitch(t *testing.T) {
	start := time.Unix(10, 0)
	g := newGyro(10, 0.5, start)
	// One second of stationary bias; alternating samples prove this is a mean.
	for i := 0; i <= 100; i++ {
		v := 100.0
		if i%2 == 1 {
			v = 102
		}
		g.sample(v, 200, start.Add(time.Duration(i)*10*time.Millisecond))
	}
	if !g.ready {
		t.Fatal("calibration did not finish")
	}
	if math.Abs(g.bias[0]-101) > 0.02 {
		t.Fatalf("bias=%v", g.bias)
	}
	now := start.Add(1010 * time.Millisecond)
	g.sample(g.bias[0]+gyroUnitsPerDegree*10, 200, now)
	g.move(now, false)
	x, y := g.move(now.Add(20*time.Millisecond), false)
	if x != 1 || y != 0 {
		t.Fatalf("motion %d %d", x, y)
	} // (10-.5)*10*.02 = 1.9
	x, y = g.move(now.Add(40*time.Millisecond), false)
	if x != 2 || y != 0 {
		t.Fatalf("fraction lost: %d %d", x, y)
	}
}
