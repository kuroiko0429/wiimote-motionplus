//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func telemetryFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func telemetryLink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
func telemetryFixture(t *testing.T) (string, deviceSet, string) {
	t.Helper()
	root := t.TempDir()
	hid := "/devices/virtual/misc/uhid/0005:057E:0306.0001"
	telemetryFile(t, root+hid+"/uevent", "HID_NAME=Nintendo Wii Remote\nHID_UNIQ=aa:bb:cc:dd:ee:ff\nHID_ID=0005:0000057E:00000306\n")
	return root, deviceSet{identity: "sysfs:" + hid}, root + hid
}
func telemetryBattery(t *testing.T, root, hid, name, value string) string {
	t.Helper()
	path := hid + "/power_supply/" + name
	telemetryFile(t, path+"/capacity", value)
	telemetryLink(t, hid, path+"/device")
	telemetryLink(t, path, root+"/class/power_supply/"+name)
	return path
}
func TestTelemetryUnknownCapacityAndIdentity(t *testing.T) {
	for _, value := range []string{"", "garbage", "-1", "101", "0", "100"} {
		t.Run("capacity="+value, func(t *testing.T) {
			root, d, hid := telemetryFixture(t)
			battery := telemetryBattery(t, root, hid, "wiimote_battery_test", value)
			info, err := readRemoteInfoAt(root, d)
			if err != nil {
				t.Fatal(err)
			}
			if value == "0" || value == "100" {
				if info.BatteryPercent == nil {
					t.Fatal("valid capacity unknown")
				}
			} else if info.BatteryPercent != nil {
				t.Fatalf("invalid capacity accepted: %v", *info.BatteryPercent)
			}
			if err := os.Remove(battery + "/capacity"); err != nil {
				t.Fatal(err)
			}
			info, err = readRemoteInfoAt(root, d)
			if err != nil || info.BatteryPercent != nil {
				t.Fatalf("missing capacity: %+v %v", info, err)
			}
			telemetryFile(t, hid+"/uevent", "HID_NAME=Remote\nHID_UNIQ=not-a-serial\nHID_ID=invalid\n")
			info, err = readRemoteInfoAt(root, d)
			if err != nil || info.MAC != "" || info.Vendor != "" || info.Product != "" {
				t.Fatalf("invented identity: %+v %v", info, err)
			}
		})
	}
}
func TestTelemetryRejectsBatteryOutsideSysfs(t *testing.T) {
	root, d, hid := telemetryFixture(t)
	outside := t.TempDir()
	telemetryFile(t, outside+"/capacity", "42")
	telemetryLink(t, hid, outside+"/device")
	telemetryLink(t, outside, root+"/class/power_supply/wiimote_battery_escape")
	info, err := readRemoteInfoAt(root, d)
	if err != nil {
		t.Fatal(err)
	}
	if info.BatteryPercent != nil || info.BatteryPath != "" {
		t.Fatalf("read outside sysfs: %+v", info)
	}
}

func TestTelemetryRejectsUnsafeIdentity(t *testing.T) {
	root, _, _ := telemetryFixture(t)
	telemetryFile(t, root+"/class/power_supply/uevent", "HID_NAME=Not HID\n")
	outside := t.TempDir()
	telemetryFile(t, outside+"/uevent", "HID_NAME=Outside sysfs\n")
	telemetryLink(t, outside, root+"/devices/escape")
	for _, id := range []string{"sysfs:/../outside", "sysfs:/devices/escape", "sysfs:/class/power_supply", "uniq:anything", "sysfs:/devices/../../outside"} {
		if _, err := readRemoteInfoAt(root, deviceSet{identity: id}); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}
func TestTelemetryResolvesEventHIDParent(t *testing.T) {
	root, d, hid := telemetryFixture(t)
	telemetryFile(t, hid+"/input/input9/name", "Nintendo Wii Remote")
	telemetryLink(t, hid+"/input/input9", root+"/class/input/event17/device")
	d.identity = "uniq:aa:bb:cc:dd:ee:ff"
	d.button = "/dev/input/event17"
	info, err := readRemoteInfoAt(root, d)
	if err != nil || info.Name != "Nintendo Wii Remote" {
		t.Fatalf("event fallback: %+v %v", info, err)
	}
	// Without a device link, the kernel power-supply parent still identifies it.
	battery := telemetryBattery(t, root, hid, "wiimote_battery_test", "25")
	if err := os.Remove(battery + "/device"); err != nil {
		t.Fatal(err)
	}
	info, err = readRemoteInfoAt(root, d)
	if err != nil || info.BatteryPercent == nil || *info.BatteryPercent != 25 {
		t.Fatalf("parent association: %+v %v", info, err)
	}
}
func TestTelemetryFormatting(t *testing.T) {
	s := formatRemoteInfo(RemoteInfo{Identity: "remote"})
	for _, want := range []string{"Identity: remote", "Bluetooth address: unknown", "Battery: unknown"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
	percent := 42
	s = formatRemoteInfo(RemoteInfo{Identity: "remote", Name: "Nintendo Wii Remote", MAC: "AA:BB:CC:DD:EE:FF", Vendor: "057E", Product: "0306", BatteryPercent: &percent})
	for _, want := range []string{"42%", "057E", "0306", "Nintendo Wii Remote", "AA:BB:CC:DD:EE:FF"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
	if strings.Contains(strings.ToLower(s), "serial") {
		t.Fatalf("MAC mislabeled: %q", s)
	}
}
func TestTelemetryWarningTransitions(t *testing.T) {
	now := time.Unix(12345, 0)
	registry := &telemetryRegistry{}
	var calls, logs []string
	percent := 50
	deps := telemetryDeps{
		read: func(deviceSet) (RemoteInfo, error) {
			return RemoteInfo{Identity: "hid-session", MAC: "AA:BB:CC:DD:EE:FF", BatteryPercent: &percent}, nil
		},
		now: func() time.Time { return now },
		notify: func(_ context.Context, urgency, summary, body string) error {
			calls = append(calls, urgency+" "+summary+" "+body)
			return nil
		},
		registry: registry,
	}
	logger := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	m := telemetryMonitor{deps: deps, options: telemetryOptions{WarningPercent: 20}, log: logger}
	tick := func(value int) {
		percent = value
		m.sample(context.Background(), deviceSet{})
		now = now.Add(time.Minute)
	}
	tick(50)
	tick(20)
	tick(19)
	tick(19)
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "normal ") {
		t.Fatalf("low spam: %v", calls)
	}
	tick(10)
	tick(9)
	if len(calls) != 2 || !strings.HasPrefix(calls[1], "critical ") {
		t.Fatalf("missing escalation: %v", calls)
	}
	// A fresh connection reuses per-Bluetooth-address state even if HID identity changes.
	m = telemetryMonitor{deps: deps, options: telemetryOptions{WarningPercent: 20}, log: logger}
	tick(9)
	if len(calls) != 2 {
		t.Fatalf("reconnect spam: %v", calls)
	}
	tick(24)
	tick(19)
	if len(calls) != 2 {
		t.Fatalf("hysteresis lost: %v", calls)
	}
	tick(26)
	tick(20)
	if len(calls) != 3 {
		t.Fatalf("not rearmed: %v", calls)
	}
	before := len(logs)
	tick(20)
	tick(20)
	if len(logs) != before {
		t.Fatalf("unchanged battery spam: %v", logs[before:])
	}
}
func TestTelemetryNotificationFailureCooldownAndUnknown(t *testing.T) {
	now := time.Unix(12345, 0)
	attempts := 0
	var percent *int
	var logs []string
	deps := telemetryDeps{read: func(deviceSet) (RemoteInfo, error) {
		return RemoteInfo{Identity: "remote", BatteryPercent: percent}, nil
	}, now: func() time.Time { return now }, registry: &telemetryRegistry{}, notify: func(context.Context, string, string, string) error {
		attempts++
		return errors.New("notify-send missing")
	}}
	m := telemetryMonitor{deps: deps, options: telemetryOptions{WarningPercent: 20}, log: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}
	m.sample(context.Background(), deviceSet{})
	if attempts != 0 {
		t.Fatal("unknown sent warning")
	}
	p := 15
	percent = &p
	m.sample(context.Background(), deviceSet{})
	m.sample(context.Background(), deviceSet{})
	if attempts != 1 {
		t.Fatalf("failure retry spam: %d", attempts)
	}
	now = now.Add(telemetryCooldown + time.Second)
	m.sample(context.Background(), deviceSet{})
	if attempts != 2 {
		t.Fatalf("did not retry after cooldown: %d", attempts)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "notify-send missing") {
		t.Fatalf("failure unreported: %v", logs)
	}
}

func TestTelemetryCriticalFailureIsRateLimited(t *testing.T) {
	now := time.Unix(12345, 0)
	p := 20
	attempts := 0
	deps := telemetryDeps{read: func(deviceSet) (RemoteInfo, error) { return RemoteInfo{Identity: "remote", BatteryPercent: &p}, nil }, now: func() time.Time { return now }, registry: &telemetryRegistry{}, notify: func(context.Context, string, string, string) error {
		attempts++
		if attempts > 1 {
			return errors.New("DBus unavailable")
		}
		return nil
	}}
	m := telemetryMonitor{deps: deps, options: telemetryOptions{WarningPercent: 20}, log: func(string, ...any) {}}
	m.sample(context.Background(), deviceSet{})
	p = 10
	m.sample(context.Background(), deviceSet{})
	m.sample(context.Background(), deviceSet{})
	if attempts != 2 {
		t.Fatalf("critical failure spam: %d", attempts)
	}
}
func TestTelemetryMonitorCancellationJoinsNotification(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	percent := 10
	deps := telemetryDeps{read: func(deviceSet) (RemoteInfo, error) {
		return RemoteInfo{Identity: "remote", BatteryPercent: &percent}, nil
	}, now: time.Now, registry: &telemetryRegistry{}, notify: func(ctx context.Context, _, _, _ string) error {
		close(entered)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}}
	stop := startTelemetryWith(context.Background(), deviceSet{}, telemetryOptions{WarningPercent: 20, Interval: time.Hour}, nil, deps)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no initial sample")
	}
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop failed to join")
	}
	select {
	case <-finished:
	default:
		t.Fatal("notification not joined")
	}
}
func TestTelemetryNotifyCommandIsBoundedAndLiteral(t *testing.T) {
	called := false
	runner := func(ctx context.Context, name string, args ...string) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("missing short timeout")
		}
		expected := []string{"-a", "wiimote-motionplus", "-u", "critical", "--", "Summary; $(false)", "Body & literal"}
		if name != "notify-send" || strings.Join(args, "\x00") != strings.Join(expected, "\x00") {
			t.Fatalf("unsafe command: %s %q", name, args)
		}
		return errors.New("mock command failed")
	}
	if err := notifyRemoteBatteryWith(context.Background(), runner, "critical", "Summary; $(false)", "Body & literal"); err == nil || !called {
		t.Fatalf("lost command error: %v", err)
	}
}

func TestTelemetryMatchesOnlySelectedRemote(t *testing.T) {
	root, d, hid := telemetryFixture(t)
	telemetryBattery(t, root, root+"/devices/laptop", "BAT1", "99")
	telemetryBattery(t, root, root+"/devices/other", "wiimote_battery_other", "88")
	expected := telemetryBattery(t, root, hid, "wiimote_battery_aa:bb:cc:dd:ee:ff", "37\n")
	info, err := readRemoteInfoAt(root, d)
	if err != nil {
		t.Fatal(err)
	}
	if info.Identity != d.identity || info.MAC != "AA:BB:CC:DD:EE:FF" || info.Name != "Nintendo Wii Remote" || info.Vendor != "057E" || info.Product != "0306" || info.BatteryPercent == nil || *info.BatteryPercent != 37 || info.BatteryPath != expected {
		t.Fatalf("wrong remote info: %+v", info)
	}
}
