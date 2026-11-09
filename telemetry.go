//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RemoteInfo contains only identity/status fields actually exposed by Linux.
// MAC is a Bluetooth address, not a manufacturing serial number.
type RemoteInfo struct {
	Identity, MAC, Name, Vendor, Product, BatteryPath string
	BatteryPercent                                    *int
}

func formatRemoteInfo(info RemoteInfo) string {
	known := func(s string) string {
		if s == "" {
			return "unknown"
		}
		return s
	}
	return fmt.Sprintf("Identity: %s; Name: %s; Bluetooth address: %s; Vendor: %s; Product: %s; Battery: %s", known(info.Identity), known(info.Name), known(info.MAC), known(info.Vendor), known(info.Product), telemetryCapacity(info.BatteryPercent))
}
func telemetryCapacity(percent *int) string {
	if percent == nil || *percent < 0 || *percent > 100 {
		return "unknown"
	}
	return fmt.Sprintf("%d%%", *percent)
}

type telemetryOptions struct {
	WarningPercent int
	Interval       time.Duration
}

const telemetryCooldown = 5 * time.Minute

type telemetryWarning struct {
	level, attemptedLevel int
	lastTry               time.Time
	pending               bool
}
type telemetryRegistry struct {
	sync.Mutex
	devices map[string]*telemetryWarning
}

var processTelemetryRegistry telemetryRegistry

type telemetryDeps struct {
	read     func(deviceSet) (RemoteInfo, error)
	now      func() time.Time
	notify   func(context.Context, string, string, string) error
	registry *telemetryRegistry
}
type telemetryMonitor struct {
	deps                   telemetryDeps
	options                telemetryOptions
	log                    func(string, ...any)
	initialized            bool
	lastBattery, lastError string
}

func (m *telemetryMonitor) sample(ctx context.Context, d deviceSet) {
	if ctx.Err() != nil {
		return
	}
	info, err := m.deps.read(d)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		if m.lastError != err.Error() {
			m.log("Wii Remote telemetry unavailable: %v", err)
			m.lastError = err.Error()
		}
		return
	}
	m.lastError = ""
	battery := telemetryCapacity(info.BatteryPercent)
	if !m.initialized {
		m.log("%s", formatRemoteInfo(info))
		m.initialized = true
	} else if battery != m.lastBattery {
		m.log("Wii Remote %s battery: %s", info.Identity, battery)
	}
	m.lastBattery = battery
	if battery == "unknown" {
		return
	}
	threshold := m.options.WarningPercent
	if threshold == 0 {
		return // Keep status logging, but explicitly disable battery notifications.
	}
	if threshold < 0 || threshold > 100 {
		threshold = 20
	}
	percent := *info.BatteryPercent
	key := info.MAC
	if key == "" {
		key = info.Identity
	}
	if key == "" {
		return
	}
	r := m.deps.registry
	r.Lock()
	if r.devices == nil {
		r.devices = map[string]*telemetryWarning{}
	}
	state := r.devices[key]
	if state == nil {
		state = &telemetryWarning{}
		r.devices[key] = state
	}
	if percent > threshold+5 {
		state.level = 0
		state.attemptedLevel = 0
	}
	level := 0
	if percent <= threshold {
		level = 1
		if percent <= 10 {
			level = 2
		}
	}
	now := m.deps.now()
	escalation := state.level == 1 && level == 2 && state.attemptedLevel < 2
	if level == 0 || level <= state.level || state.pending || (!escalation && !state.lastTry.IsZero() && now.Sub(state.lastTry) < telemetryCooldown) {
		r.Unlock()
		return
	}
	state.lastTry = now
	state.attemptedLevel = level
	state.pending = true
	r.Unlock()
	urgency := "normal"
	summary := "Wii Remote battery low"
	if level == 2 {
		urgency = "critical"
		summary = "Wii Remote battery critically low"
	}
	address := info.MAC
	if address == "" {
		address = info.Identity
	}
	err = m.deps.notify(ctx, urgency, summary, fmt.Sprintf("%s: %d%% battery remaining.", address, percent))
	r.Lock()
	state.pending = false
	if err == nil {
		state.level = level
	}
	r.Unlock()
	if err != nil && ctx.Err() == nil {
		m.log("Wii Remote battery notification unavailable: %v", err)
	}
}

// startTelemetry starts a read-only sysfs monitor. Stop cancels and joins it;
// a sysfs capacity read already in progress must finish in the kernel first.
func startTelemetry(parent context.Context, d deviceSet, o telemetryOptions, log func(string, ...any)) func() {
	return startTelemetryWith(parent, d, o, log, telemetryDeps{read: readRemoteInfo, now: time.Now, notify: notifyRemoteBattery, registry: &processTelemetryRegistry})
}
func startTelemetryWith(parent context.Context, d deviceSet, o telemetryOptions, log func(string, ...any), deps telemetryDeps) func() {
	if o.Interval <= 0 {
		o.Interval = time.Minute
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor := telemetryMonitor{deps: deps, options: o, log: log}
		monitor.sample(ctx, d)
		ticker := time.NewTicker(o.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				monitor.sample(ctx, d)
			}
		}
	}()
	return func() { cancel(); <-done }
}
func notifyRemoteBattery(ctx context.Context, urgency, summary, body string) error {
	return notifyRemoteBatteryWith(ctx, func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run()
	}, urgency, summary, body)
}
func notifyRemoteBatteryWith(parent context.Context, run func(context.Context, string, ...string) error, urgency, summary, body string) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	return run(ctx, "notify-send", "-a", "wiimote-motionplus", "-u", urgency, "--", summary, body)
}

func readRemoteInfo(d deviceSet) (RemoteInfo, error) { return readRemoteInfoAt("/sys", d) }

var telemetryMAC = regexp.MustCompile(`(?i)^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

var telemetryEvent = regexp.MustCompile(`^event[0-9]+$`)

func telemetryHIDPath(root string, d deviceSet) (string, error) {
	root = filepath.Clean(root)
	if strings.HasPrefix(d.identity, "sysfs:") {
		rel := strings.TrimPrefix(d.identity, "sysfs:")
		if !strings.HasPrefix(rel, "/devices/") || filepath.Clean(rel) != rel {
			return "", fmt.Errorf("invalid HID sysfs identity")
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(path, filepath.Join(root, "devices")+string(os.PathSeparator)) {
			return "", fmt.Errorf("HID path outside sysfs devices")
		}
		return path, nil
	}
	for _, event := range []string{d.button, d.accel, d.gyro, d.motion} {
		name := filepath.Base(event)
		if filepath.Dir(event) != "/dev/input" || !telemetryEvent.MatchString(name) {
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, "class/input", name, "device"))
		if err != nil {
			continue
		}
		for strings.HasPrefix(path, filepath.Join(root, "devices")+string(os.PathSeparator)) {
			data, err := os.ReadFile(filepath.Join(path, "uevent"))
			if err == nil && strings.Contains("\n"+string(data), "\nHID_ID=") {
				return path, nil
			}
			path = filepath.Dir(path)
		}
	}
	return "", fmt.Errorf("Wii Remote HID identity unavailable")
}

func readRemoteInfoAt(root string, d deviceSet) (RemoteInfo, error) {
	info := RemoteInfo{Identity: d.identity}
	path, err := telemetryHIDPath(root, d)
	if err != nil {
		return info, err
	}
	data, err := os.ReadFile(filepath.Join(path, "uevent"))
	if err != nil {
		return info, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "HID_NAME":
			info.Name = value
		case "HID_UNIQ":
			if telemetryMAC.MatchString(value) {
				info.MAC = strings.ToUpper(value)
			}
		case "HID_ID":
			fields := strings.Split(value, ":")
			if len(fields) == 3 {
				vendor, e1 := strconv.ParseUint(fields[1], 16, 16)
				product, e2 := strconv.ParseUint(fields[2], 16, 16)
				if e1 == nil && e2 == nil {
					info.Vendor = fmt.Sprintf("%04X", vendor)
					info.Product = fmt.Sprintf("%04X", product)
				}
			}
		}
	}
	entries, _ := filepath.Glob(filepath.Join(root, "class/power_supply/wiimote_battery_*"))
	for _, entry := range entries {
		battery, e := filepath.EvalSymlinks(entry)
		if e != nil || !strings.HasPrefix(battery, filepath.Join(root, "devices")+string(os.PathSeparator)) {
			continue
		}
		parent, e := filepath.EvalSymlinks(filepath.Join(battery, "device"))
		if os.IsNotExist(e) {
			parent = filepath.Dir(filepath.Dir(battery))
			e = nil
		}
		if e != nil || parent != path {
			continue
		}
		info.BatteryPath = battery
		data, e := os.ReadFile(filepath.Join(battery, "capacity"))
		if e == nil {
			percent, e := strconv.Atoi(strings.TrimSpace(string(data)))
			if e == nil && percent >= 0 && percent <= 100 {
				info.BatteryPercent = &percent
			}
		}
		break
	}
	return info, nil
}
