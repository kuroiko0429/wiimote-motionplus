//go:build linux

package main

import (
	"math"
	"time"
)

// hid-wiimote normalizes fast/slow ranges and removes the 8192 center.
// ABS_RX is yaw, ABS_RY roll (unused), ABS_RZ pitch.
const gyroUnitsPerDegree = 8192.0 * 9 / 440
const gyroFreshness = 100 * time.Millisecond

type gyro struct {
	sensitivity, deadzone       float64
	bias, rate, sum, carry      [2]float64
	count                       int
	start, lastSample, lastMove time.Time
	ready                       bool
}

func newGyro(sensitivity, deadzone float64, now time.Time) *gyro {
	g := &gyro{sensitivity: sensitivity, deadzone: deadzone}
	g.calibrate(now)
	return g
}
func (g *gyro) calibrate(now time.Time) {
	g.rate = [2]float64{}
	g.start = now
	g.ready = false
	g.count = 0
	g.sum = [2]float64{}
	g.carry = [2]float64{}
	g.lastMove = time.Time{}
	g.lastSample = time.Time{}
}
func (g *gyro) sample(yaw, pitch float64, now time.Time) {
	if !g.ready && !g.lastSample.IsZero() && now.Sub(g.lastSample) > gyroFreshness {
		g.calibrate(now)
	}
	g.lastSample = now
	if !g.ready {
		if g.count == 0 {
			g.start = now
		}
		g.sum[0] += yaw
		g.sum[1] += pitch
		g.count++
		if now.Sub(g.start) >= time.Second && g.count >= 20 {
			for i := range g.bias {
				g.bias[i] = g.sum[i] / float64(g.count)
			}
			g.ready = true
		}
		return
	}
	for i, v := range [2]float64{yaw, pitch} {
		r := (v - g.bias[i]) / gyroUnitsPerDegree
		if math.Abs(r) <= g.deadzone {
			r = 0
		} else {
			r -= math.Copysign(g.deadzone, r)
		}
		g.rate[i] = r
	}
}
func (g *gyro) move(now time.Time, paused bool) (int32, int32) {
	dt := now.Sub(g.lastMove)
	g.lastMove = now
	if paused || !g.ready || g.lastSample.IsZero() || now.Sub(g.lastSample) > gyroFreshness || dt <= 0 || dt > gyroFreshness {
		g.carry = [2]float64{}
		return 0, 0
	}
	var out [2]int32
	for i := range out {
		g.carry[i] += g.rate[i] * g.sensitivity * dt.Seconds()
		out[i] = int32(g.carry[i])
		g.carry[i] -= float64(out[i])
	}
	return out[0], out[1]
}
