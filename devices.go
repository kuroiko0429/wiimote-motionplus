//go:build linux

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var handlerRe = regexp.MustCompile(`\bevent\d+\b`)
var inputSuffixRe = regexp.MustCompile(`/input/input\d+$`)

type deviceSet struct{ identity, accel, gyro, button, motion, mode string }

// Sysfs is shared even by MotionPlus nodes whose phys and uniq are empty.
// Never group unidentified nodes together: that could combine two remotes.
func parseDevices(text string) []deviceSet {
	var groups []deviceSet
	indices := map[string]int{}
	for _, block := range strings.Split(text, "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if len(line) >= 3 {
				fields[line[:2]] = strings.TrimSpace(line[3:])
			}
		}
		name := strings.Trim(strings.TrimPrefix(fields["N:"], "Name="), "\"")
		if name != "Nintendo Wii Remote" && name != "Nintendo Wii Remote Accelerometer" && name != "Nintendo Wii Remote Motion Plus" {
			continue
		}
		identity := ""
		if s := strings.TrimPrefix(fields["S:"], "Sysfs="); inputSuffixRe.MatchString(s) {
			identity = "sysfs:" + inputSuffixRe.ReplaceAllString(s, "")
		}
		if identity == "" {
			if u := strings.TrimPrefix(fields["U:"], "Uniq="); u != "" {
				identity = "uniq:" + strings.ToLower(u)
			}
		}
		if identity == "" {
			if p := strings.TrimPrefix(fields["P:"], "Phys="); p != "" {
				identity = "phys:" + strings.TrimSuffix(p, "/input0")
			}
		}
		event := handlerRe.FindString(strings.TrimPrefix(fields["H:"], "Handlers="))
		if identity == "" || event == "" {
			continue
		}
		i, ok := indices[identity]
		if !ok {
			i = len(groups)
			indices[identity] = i
			groups = append(groups, deviceSet{identity: identity})
		}
		path := "/dev/input/" + event
		switch name {
		case "Nintendo Wii Remote":
			groups[i].button = path
		case "Nintendo Wii Remote Accelerometer":
			groups[i].accel = path
		case "Nintendo Wii Remote Motion Plus":
			groups[i].gyro = path
		}
	}
	return groups
}

func selectDevices(groups []deviceSet, mode string) (deviceSet, error) {
	if mode != "accel" {
		for _, d := range groups {
			if d.button != "" && d.gyro != "" {
				d.mode = "gyro"
				d.motion = d.gyro
				return d, nil
			}
		}
	}
	if mode != "gyro" {
		for _, d := range groups {
			if d.button != "" && d.accel != "" {
				d.mode = "accel"
				d.motion = d.accel
				return d, nil
			}
		}
	}
	return deviceSet{}, fmt.Errorf("no matching Wii Remote input devices for mode %s (gyro requires MotionPlus)", mode)
}

func scanDevices() ([]deviceSet, error) {
	data, err := os.ReadFile("/proc/bus/input/devices")
	return parseDevices(string(data)), err
}

// Auto waits for late MotionPlus enumeration, then falls back at the deadline.
func waitDevices(mode string, timeout time.Duration, scan func() ([]deviceSet, error), now func() time.Time, sleep func(time.Duration)) (deviceSet, error) {
	deadline := now().Add(timeout)
	for {
		groups, err := scan()
		if err != nil {
			return deviceSet{}, err
		}
		d, err := selectDevices(groups, mode)
		if err == nil && (mode != "auto" || d.mode == "gyro" || !now().Before(deadline)) {
			return d, nil
		}
		if !now().Before(deadline) {
			return deviceSet{}, err
		}
		remaining := deadline.Sub(now())
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		sleep(remaining)
	}
}

// Legacy wrappers retained for callers using accelerometer discovery.
func findWiimoteDevices() (string, string, error) {
	groups, err := scanDevices()
	if err != nil {
		return "", "", err
	}
	d, err := selectDevices(groups, "accel")
	return d.motion, d.button, err
}
func waitForWiimoteDevices(timeout time.Duration) (string, string, error) {
	d, err := waitDevices("accel", timeout, scanDevices, time.Now, time.Sleep)
	return d.motion, d.button, err
}
