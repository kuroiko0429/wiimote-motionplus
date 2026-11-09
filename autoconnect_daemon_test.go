//go:build linux

package main

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestDaemonCancelsConnectorBeforeSessionWithoutBlockingPoll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := make(chan struct{})
	stopped := make(chan struct{})
	scans := 0
	deps := daemonDeps{now: time.Now, sleep: sleepContext, log: func(string, ...any) {}, load: func(string) (map[uint16]string, error) { return defaultBindings(), nil }}
	deps.connect = func(ctx context.Context) { close(started); <-ctx.Done(); close(stopped) }
	deps.scan = func() ([]deviceSet, error) {
		scans++
		if scans == 1 {
			return nil, nil
		}
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("connector not started")
		}
		return []deviceSet{{button: "b", gyro: "g"}}, nil
	}
	deps.session = func(context.Context, deviceSet, options, map[uint16]string) error {
		select {
		case <-stopped:
		default:
			t.Error("connector not joined")
		}
		cancel()
		return io.EOF
	}
	if err := daemonLoop(ctx, options{mode: "gyro", autoConnect: true}, deps); err != nil {
		t.Fatal(err)
	}
	if scans != 2 {
		t.Fatalf("poll blocked: %d scans", scans)
	}
}
func TestDaemonSkipsBluetoothWhenConnectedOrOptedOut(t *testing.T) {
	for _, connected := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		deps := daemonDeps{connect: func(context.Context) { t.Error("unwanted Bluetooth worker") }, now: time.Now, log: func(string, ...any) {}, load: func(string) (map[uint16]string, error) { return defaultBindings(), nil }}
		deps.scan = func() ([]deviceSet, error) {
			if connected {
				return []deviceSet{{button: "b", gyro: "g"}}, nil
			}
			return nil, nil
		}
		deps.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
		deps.session = func(context.Context, deviceSet, options, map[uint16]string) error { cancel(); return nil }
		if err := daemonLoop(ctx, options{mode: "gyro", autoConnect: connected}, deps); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}
func TestAutoConnectOptions(t *testing.T) {
	o, err := parseOptions([]string{"--daemon"}, io.Discard)
	if err != nil || !o.autoConnect {
		t.Fatalf("default %+v %v", o, err)
	}
	o, err = parseOptions([]string{"--daemon", "--auto-connect=false"}, io.Discard)
	if err != nil || o.autoConnect {
		t.Fatalf("opt out %+v %v", o, err)
	}
}
