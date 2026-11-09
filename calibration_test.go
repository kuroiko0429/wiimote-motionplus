package main

import (
	"testing"
	"time"
)

func TestCalibrationStartsWhenSamplesActuallyArrive(t *testing.T) {
	n := time.Unix(10, 0)
	g := newGyro(10, 0, n)
	for i := 0; i < 30; i++ {
		g.sample(100, 200, n.Add(10*time.Second+time.Duration(i)*10*time.Millisecond))
	}
	if g.ready {
		t.Fatal("less than one second of samples calibrated")
	}
}
