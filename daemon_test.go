//go:build linux

package main

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestDaemonAutoGraceStartsAtAccelEnumeration(t *testing.T) {
	for _, lateGyro := range []bool{false, true} {
		t.Run(fmt.Sprint(lateGyro), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(1, 0)
			start := now
			var ready time.Time
			deps := daemonDeps{now: func() time.Time { return now }, sleep: func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }, log: func(string, ...any) {}, load: func(string) (map[uint16]string, error) { return defaultBindings(), nil }}
			deps.scan = func() ([]deviceSet, error) {
				if now.Sub(start) < 3*time.Second {
					return nil, nil
				}
				if ready.IsZero() {
					ready = now
				}
				d := deviceSet{identity: "remote", button: "buttons", accel: "accel"}
				if lateGyro && now.Sub(ready) >= 600*time.Millisecond {
					d.gyro = "gyro"
				}
				return []deviceSet{d}, nil
			}
			deps.session = func(_ context.Context, d deviceSet, _ options, _ map[uint16]string) error {
				want := time.Second
				mode := "accel"
				if lateGyro {
					want = 600 * time.Millisecond
					mode = "gyro"
				}
				if now.Sub(ready) != want || d.mode != mode {
					t.Fatalf("grace=%s mode=%s", now.Sub(ready), d.mode)
				}
				cancel()
				return nil
			}
			if err := daemonLoop(ctx, options{mode: "auto"}, deps); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDaemonTransientFailuresBackOffAndLimitLogs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Unix(1, 0)
	var delays []time.Duration
	attempts, logs := 0, 0
	deps := daemonDeps{scan: func() ([]deviceSet, error) { return []deviceSet{{button: "b", gyro: "g"}}, nil }, load: func(string) (map[uint16]string, error) { return defaultBindings(), nil }, now: func() time.Time { return now }, log: func(string, ...any) { logs++ }}
	deps.session = func(context.Context, deviceSet, options, map[uint16]string) error {
		attempts++
		if attempts == 8 {
			cancel()
		}
		return fmt.Errorf("permission denied")
	}
	deps.sleep = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		now = now.Add(d)
		return nil
	}
	if err := daemonLoop(ctx, options{mode: "gyro"}, deps); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond, 2 * time.Second, 2 * time.Second, 2 * time.Second}
	if !reflect.DeepEqual(delays, want) || logs > 3 {
		t.Fatalf("delays=%v logs=%d", delays, logs)
	}
}

func TestDaemonReconnectReloadsConfigAndRescansPaths(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scans, loads, sessions := 0, 0, 0
	var paths []string
	now := time.Unix(1, 0)
	deps := daemonDeps{
		scan: func() ([]deviceSet, error) {
			scans++
			if scans == 1 {
				return nil, nil
			}
			return []deviceSet{{button: "buttons", gyro: fmt.Sprint(scans)}}, nil
		},
		load: func(string) (map[uint16]string, error) {
			loads++
			return map[uint16]string{btnA: fmt.Sprint(loads)}, nil
		},
		session: func(ctx context.Context, d deviceSet, o options, b map[uint16]string) error {
			sessions++
			paths = append(paths, d.motion)
			if b[btnA] != fmt.Sprint(sessions) {
				t.Fatal("stale mapping")
			}
			if sessions == 2 {
				cancel()
			}
			return io.EOF
		},
		now: func() time.Time { return now },
		sleep: func(ctx context.Context, d time.Duration) error {
			if d < 100*time.Millisecond {
				t.Fatal("tight retry")
			}
			now = now.Add(d)
			return ctx.Err()
		},
		log: func(string, ...any) {},
	}
	if err := daemonLoop(ctx, options{mode: "gyro"}, deps); err != nil {
		t.Fatal(err)
	}
	if loads != 2 || sessions != 2 || !reflect.DeepEqual(paths, []string{"2", "3"}) {
		t.Fatalf("loads=%d sessions=%d paths=%v", loads, sessions, paths)
	}
}
