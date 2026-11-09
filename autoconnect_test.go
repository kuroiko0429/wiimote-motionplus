//go:build linux

package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestOnlyExactWiiNamesAndMACsAreAccepted(t *testing.T) {
	lines := "Device 02:11:22:33:44:55 Nintendo RVL-CNT-01\n[NEW] Device AA:BB:CC:DD:EE:FF Nintendo RVL-CNT-01-TR\nDevice 11:22:33:44:55:66 Nintendo RVL-CNT-01 evil\nDevice 11:22:33:44:55:67 NotNintendo RVL-CNT-01\nDevice X11:22:33:44:55:68 Nintendo RVL-CNT-01\nDevice 11:22:33:44:55:69 Nintendo RVL-CNT-01-TRextra\n"
	want := []string{"02:11:22:33:44:55", "AA:BB:CC:DD:EE:FF"}
	if got := namedWiiRemotes(lines); !reflect.DeepEqual(got, want) {
		t.Fatalf("accepted %v, want %v", got, want)
	}
	// Existing one-shot collector must share the same strict name boundary.
	for _, line := range []string{"Device 11:22:33:44:55:66 Nintendo RVL-CNT-01 evil", "Device X11:22:33:44:55:68 Nintendo RVL-CNT-01"} {
		ch := make(chan string, 1)
		ch <- line
		close(ch)
		if got := collectMac(ch, 1); got != "" {
			t.Fatalf("unsafe one-shot match %s", got)
		}
	}
}

func TestAutoConnectRotatesOfflineCachedRemotesAndScans(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts []string
	scans := 0
	powered := 0
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "power":
			powered++
		case "devices":
			return []byte("Device AA:BB:CC:DD:EE:01 Nintendo RVL-CNT-01\nDevice AA:BB:CC:DD:EE:02 Nintendo RVL-CNT-01-TR\nDevice AA:BB:CC:DD:EE:03 Other"), nil
		case "connect":
			attempts = append(attempts, args[1])
			if len(attempts) == 3 {
				cancel()
			}
		case "--timeout":
			scans++
		}
		return nil, nil
	}
	autoConnectLoop(ctx, run, func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if powered != 1 {
		t.Fatalf("adapter powered %d times, want once per waiting worker", powered)
	}
	if !reflect.DeepEqual(attempts, []string{"AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02", "AA:BB:CC:DD:EE:01"}) || scans < 2 {
		t.Fatalf("attempts=%v scans=%d", attempts, scans)
	}
}
