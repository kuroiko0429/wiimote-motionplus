//go:build linux

package main

import (
	"fmt"
	"math"
	"time"
)

type mouseRuntime struct {
	mode                  string
	gyro                  *gyro
	axes, pending, center [2]int32
	centered, paused      bool
	sensitivity           float64
	lastFrame             time.Time
	bindings              map[uint16]string
	down                  map[uint16]bool
	scroll                [4]bool
	emit                  func(rawEvent)
}

func newRuntime(mode string, sensitivity, deadzone float64, now time.Time, emit func(rawEvent)) *mouseRuntime {
	return &mouseRuntime{mode: mode, gyro: newGyro(sensitivity, deadzone, now), sensitivity: sensitivityDefault, bindings: defaultBindings(), down: make(map[uint16]bool), emit: emit}
}

// initializeAxes gets evdev's current state: unchanged axes need not be emitted.
func (r *mouseRuntime) initializeAxes(readAbs func(uint16) (int32, error)) error {
	codes := [2]uint16{absRX, absRY}
	if r.mode == "gyro" {
		codes[1] = absRZ
	}
	var axes [2]int32
	for i, code := range codes {
		v, err := readAbs(code)
		if err != nil {
			return fmt.Errorf("read initial axis %d: %w", code, err)
		}
		axes[i] = v
	}
	r.axes = axes
	r.pending = axes
	return nil
}

func (r *mouseRuntime) send(typ, code uint16, v int32) {
	r.emit(rawEvent{typ, code, v})
	r.emit(rawEvent{evSyn, 0, 0})
}
func (r *mouseRuntime) event(te taggedEvent, now time.Time) error {
	if te.err != nil {
		return fmt.Errorf("input source %d disconnected: %w", te.src, te.err)
	}
	e := te.ev
	if e.typ == evSyn && e.code == 3 {
		return fmt.Errorf("input source %d SYN_DROPPED: events lost; restart to resynchronize safely", te.src)
	}
	if te.src == srcAccel {
		if e.typ == evAbs {
			if e.code == absRX {
				r.pending[0] = e.value
			}
			if (r.mode == "gyro" && e.code == absRZ) || (r.mode == "accel" && e.code == absRY) {
				r.pending[1] = e.value
			}
		}
		if e.typ == evSyn && e.code == 0 {
			r.axes = r.pending
			r.lastFrame = now
			if r.mode == "gyro" {
				r.gyro.sample(float64(r.axes[0]), float64(r.axes[1]), now)
			} else if !r.centered {
				r.center = r.axes
				r.centered = true
			}
		}
		return nil
	}
	if e.typ != evKey {
		return nil
	}
	// Track physical sources, not key-repeat counts. Only genuine transitions
	// dispatch actions, and shared holds end only after the last source releases.
	if (e.value != 0 && e.value != 1) || r.down[e.code] == (e.value == 1) {
		return nil
	}
	r.down[e.code] = e.value == 1
	action := r.bindings[e.code]
	switch action {
	case "left_click", "right_click", "scroll_up", "scroll_down", "scroll_left", "scroll_right":
		for code, down := range r.down {
			if down && code != e.code && r.bindings[code] == action {
				return nil
			}
		}
	}
	switch action {
	case "left_click":
		r.send(evKey, btnLeft, e.value)
	case "right_click":
		r.send(evKey, btnRight, e.value)
	case "middle_click":
		if e.value == 1 {
			r.send(evKey, btnMiddle, 1)
			r.send(evKey, btnMiddle, 0)
		}
	case "calibrate":
		if e.value == 1 {
			if r.mode == "gyro" {
				r.gyro.calibrate(now)
				fmt.Println("Gyro calibration: keep remote stationary for one second.")
			} else {
				r.center = r.axes
				r.centered = !r.lastFrame.IsZero()
			}
		}
	case "pause":
		if e.value == 1 {
			r.paused = !r.paused
			r.gyro.carry = [2]float64{}
			r.gyro.lastMove = time.Time{}
		}
	case "scroll_up":
		r.scroll[0] = e.value != 0
	case "scroll_down":
		r.scroll[1] = e.value != 0
	case "scroll_left":
		r.scroll[2] = e.value != 0
	case "scroll_right":
		r.scroll[3] = e.value != 0
	case "sensitivity_up", "sensitivity_down":
		if e.value == 1 {
			sign := 1.0
			if r.bindings[e.code] == "sensitivity_down" {
				sign = -1
			}
			if r.mode == "gyro" {
				r.gyro.sensitivity = math.Max(0.1, math.Min(100, r.gyro.sensitivity+sign))
				fmt.Printf("Gyro sensitivity: %.1f px/degree\n", r.gyro.sensitivity)
			} else {
				r.sensitivity = math.Max(sensitivityMin, math.Min(sensitivityMax, r.sensitivity+sign*sensitivityStep))
				fmt.Printf("Sensitivity: %.2f\n", r.sensitivity)
			}
		}
	}
	return nil
}
func (r *mouseRuntime) move(now time.Time) {
	var x, y int32
	if r.mode == "gyro" {
		// evdev may suppress unchanged stationary ABS values (and their SYN).
		// Sample-and-hold only during bias calibration. Once ready, only actual
		// input frames refresh the rate/freshness watchdog.
		if !r.gyro.ready && !r.lastFrame.IsZero() {
			r.gyro.sample(float64(r.axes[0]), float64(r.axes[1]), now)
		}
		x, y = r.gyro.move(now, r.paused)
		// Wii Remote yaw has the opposite sign to desktop horizontal motion.
		x = -x
	} else if r.centered && !r.paused {
		x = applyDeadzone(r.axes[0]-r.center[0], r.sensitivity)
		y = applyDeadzone(r.axes[1]-r.center[1], r.sensitivity)
	}
	if x != 0 || y != 0 {
		r.emit(rawEvent{evRel, relX, x})
		r.emit(rawEvent{evRel, relY, y})
		r.emit(rawEvent{evSyn, 0, 0})
	}
}
func (r *mouseRuntime) scrollTick() {
	if r.paused {
		return
	}
	for i, v := range r.scroll {
		if v {
			code := uint16(relWheel)
			value := int32(1)
			if i >= 2 {
				code = relHWheel
			}
			if i == 1 || i == 2 {
				value = -1
			}
			r.send(evRel, code, value)
		}
	}
}
