package main

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func mappedRuntime(t *testing.T, config string) (*mouseRuntime, *[]rawEvent) {
	t.Helper()
	var out []rawEvent
	r := newRuntime("gyro", 10, 0, time.Unix(10, 0), func(e rawEvent) { out = append(out, e) })
	bindings, err := loadButtonConfig(configFile(t, config))
	if err != nil {
		t.Fatal(err)
	}
	r.bindings = bindings
	return r, &out
}
func press(r *mouseRuntime, code uint16, value int32) {
	r.event(taggedEvent{src: srcButton, ev: rawEvent{evKey, code, value}}, time.Unix(11, 0))
}
func TestSharedHoldActionsRetainUntilLastSourceRelease(t *testing.T) {
	for _, action := range []string{"left_click", "right_click", "scroll_up", "scroll_down", "scroll_left", "scroll_right"} {
		t.Run(action, func(t *testing.T) {
			r, out := mappedRuntime(t, `{"buttons":{"a":"`+action+`","b":"`+action+`"}}`)
			press(r, btnA, 1)
			press(r, btnA, 1)
			press(r, btnA, 2)
			press(r, btnB, 1)
			press(r, btnA, 0)
			press(r, btnA, 0)
			if action == "left_click" || action == "right_click" {
				if len(*out) != 2 || (*out)[0].value != 1 {
					t.Fatalf("premature release or duplicate: %v", *out)
				}
				press(r, btnB, 0)
				if len(*out) != 4 || (*out)[2].value != 0 {
					t.Fatalf("final release: %v", *out)
				}
			} else {
				r.scrollTick()
				if len(*out) != 2 {
					t.Fatalf("scroll released early: %v", *out)
				}
				press(r, btnB, 0)
				r.scrollTick()
				if len(*out) != 2 {
					t.Fatalf("scroll stuck: %v", *out)
				}
			}
		})
	}
}

func TestEveryButtonCanTriggerEveryAction(t *testing.T) {
	actions := []string{"none", "left_click", "right_click", "middle_click", "calibrate", "pause", "scroll_up", "scroll_down", "scroll_left", "scroll_right", "sensitivity_up", "sensitivity_down"}
	for name, code := range buttonCodes {
		for _, action := range actions {
			t.Run(name+"/"+action, func(t *testing.T) {
				r, out := mappedRuntime(t, `{"buttons":{"`+name+`":"`+action+`"}}`)
				r.gyro.ready = true
				press(r, code, 2) // orphan repeat must not count as a press
				press(r, code, 1)
				press(r, code, 1)
				press(r, code, 2)
				switch action {
				case "none":
					if len(*out) != 0 {
						t.Fatalf("disabled emitted %v", *out)
					}
				case "left_click", "right_click":
					want := uint16(btnLeft)
					if action == "right_click" {
						want = btnRight
					}
					if len(*out) != 2 || (*out)[0] != (rawEvent{evKey, want, 1}) {
						t.Fatalf("hold: %v", *out)
					}
				case "middle_click":
					if len(*out) != 4 || (*out)[0] != (rawEvent{evKey, btnMiddle, 1}) || (*out)[2] != (rawEvent{evKey, btnMiddle, 0}) {
						t.Fatalf("tap: %v", *out)
					}
				case "pause":
					if !r.paused {
						t.Fatal("pause toggled more than once")
					}
				case "calibrate":
					if r.gyro.ready {
						t.Fatal("not calibrated")
					}
					r.gyro.ready = true
					press(r, code, 1)
					press(r, code, 2)
					if !r.gyro.ready {
						t.Fatal("duplicate recalibrated")
					}
				case "sensitivity_up", "sensitivity_down":
					want := 11.0
					if action == "sensitivity_down" {
						want = 9
					}
					if r.gyro.sensitivity != want {
						t.Fatalf("sensitivity: %v", r.gyro.sensitivity)
					}
				default:
					r.scrollTick()
					r.scrollTick()
					wheel := uint16(relWheel)
					value := int32(1)
					if action == "scroll_left" || action == "scroll_right" {
						wheel = relHWheel
					}
					if action == "scroll_left" || action == "scroll_down" {
						value = -1
					}
					if len(*out) != 4 || (*out)[0] != (rawEvent{evRel, wheel, value}) {
						t.Fatalf("scroll: %v", *out)
					}
				}
				press(r, code, 0)
				if action == "pause" {
					press(r, code, 1)
					if r.paused {
						t.Fatal("fresh press failed to resume")
					}
				}
			})
		}
	}
}

func TestMappedAccelCalibrationAndSensitivity(t *testing.T) {
	r, _ := mappedRuntime(t, `{"buttons":{"a":"calibrate","b":"sensitivity_up","two":"sensitivity_down"}}`)
	r.mode = "accel"
	r.axes = [2]int32{20, 30}
	r.lastFrame = time.Unix(10, 0)
	press(r, btnA, 1)
	if !r.centered || r.center != r.axes {
		t.Fatalf("center: %v", r.center)
	}
	press(r, btnB, 1)
	press(r, btnB, 1)
	press(r, btnB, 2)
	if math.Abs(r.sensitivity-(sensitivityDefault+sensitivityStep)) > 1e-12 {
		t.Fatalf("sensitivity up: %v", r.sensitivity)
	}
	press(r, btn2, 1)
	if math.Abs(r.sensitivity-sensitivityDefault) > 1e-12 {
		t.Fatalf("sensitivity down: %v", r.sensitivity)
	}
}

func TestConfiguredClicksAndDisabledButtonWhilePaused(t *testing.T) {
	r, out := mappedRuntime(t, `{"buttons":{"a":"right_click","b":"none","one":"left_click"}}`)
	r.paused = true
	press(r, btnA, 1)
	press(r, btnA, 0)
	press(r, btnB, 1)
	press(r, btnB, 0)
	press(r, btn1, 1)
	press(r, btn1, 0)
	want := []rawEvent{{evKey, btnRight, 1}, {evSyn, 0, 0}, {evKey, btnRight, 0}, {evSyn, 0, 0}, {evKey, btnLeft, 1}, {evSyn, 0, 0}, {evKey, btnLeft, 0}, {evSyn, 0, 0}}
	if !reflect.DeepEqual(*out, want) {
		t.Fatalf("got %v want %v", *out, want)
	}
}
