//go:build linux

package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"time"
)

type options struct {
	configPath                           string
	mode                                 string
	gyroSensitivity, gyroDeadzone        float64
	connect, daemon, autoConnect, status bool
	batteryWarning                       int
	batteryInterval                      time.Duration
}

func parseOptions(args []string, out io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("wiimote-mouse", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.configPath, "config", "", "button JSON config (default: $XDG_CONFIG_HOME/wiimote-motionplus/config.json or ~/.config/wiimote-motionplus/config.json)")
	fs.StringVar(&o.mode, "mode", "auto", "motion source: auto (prefer MotionPlus), gyro, accel")
	fs.Float64Var(&o.gyroSensitivity, "gyro-sensitivity", 10, "gyro sensitivity in pixels per degree (0 < value <= 100)")
	fs.Float64Var(&o.gyroDeadzone, "gyro-deadzone", 0.5, "gyro deadzone in degrees/second (0..100)")
	fs.BoolVar(&o.status, "status", false, "show connected Wii Remote information and battery; does not connect or acquire the mouse lock")
	fs.IntVar(&o.batteryWarning, "battery-warning", 20, "low battery notification threshold percent (0 disables, 0..100)")
	fs.DurationVar(&o.batteryInterval, "battery-interval", time.Minute, "battery polling interval (minimum 5s)")
	fs.BoolVar(&o.connect, "connect", false, "pair and connect a new Wii Remote")
	fs.BoolVar(&o.daemon, "daemon", false, "wait and reconnect indefinitely in foreground")
	fs.BoolVar(&o.autoConnect, "auto-connect", true, "automatically connect exact-name Wii remotes while daemon waits (trusts Wii remotes only)")
	fs.BoolVar(&o.daemon, "d", false, "alias for --daemon")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	var configPathError error
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" && o.configPath == "" {
			configPathError = fmt.Errorf("--config requires a nonempty path")
		}
	})
	if configPathError != nil {
		return o, configPathError
	}
	if o.batteryWarning < 0 || o.batteryWarning > 100 {
		return o, fmt.Errorf("--battery-warning must be 0..100")
	}
	if o.batteryInterval < 5*time.Second {
		return o, fmt.Errorf("--battery-interval must be at least 5s")
	}
	if o.status && (o.daemon || o.connect) {
		return o, fmt.Errorf("--status cannot be combined with --daemon or --connect")
	}
	if o.daemon && o.connect {
		return o, fmt.Errorf("--daemon cannot be combined with --connect; connect separately first")
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if o.mode != "auto" && o.mode != "gyro" && o.mode != "accel" {
		return o, fmt.Errorf("--mode must be auto, gyro, or accel")
	}
	for _, p := range []struct {
		name     string
		v        float64
		positive bool
	}{{"gyro-sensitivity", o.gyroSensitivity, true}, {"gyro-deadzone", o.gyroDeadzone, false}} {
		if math.IsNaN(p.v) || math.IsInf(p.v, 0) || p.v < 0 || p.v > 100 || (p.positive && p.v == 0) {
			return o, fmt.Errorf("invalid --%s: must be finite, %s and <= 100", p.name, map[bool]string{true: "positive", false: "nonnegative"}[p.positive])
		}
	}
	return o, nil
}
