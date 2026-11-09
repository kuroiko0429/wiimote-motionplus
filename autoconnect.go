//go:build linux

package main

import (
	"context"
	"os/exec"
	"time"
)

type bluetoothCommand func(context.Context, ...string) ([]byte, error)

func runBluetoothCommand(parent context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bluetoothctl", args...)
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

// This worker never owns input polling. Each command and delay is cancellable;
// the daemon cancels and joins it before running an available input session.
// Only exact advertised Wii names are eligible for automatic trust/connect.
func autoConnectLoop(ctx context.Context, run bluetoothCommand, sleep func(context.Context, time.Duration) error) {
	next := 0
	if ctx.Err() != nil {
		return
	}
	_, _ = run(ctx, "power", "on")
	delay := 2 * time.Second
	for ctx.Err() == nil {
		output, _ := run(ctx, "devices")
		remotes := namedWiiRemotes(string(output))
		if len(remotes) > 0 && ctx.Err() == nil {
			mac := remotes[next%len(remotes)]
			next++
			if _, err := run(ctx, "trust", mac); err == nil && ctx.Err() == nil {
				_, _ = run(ctx, "connect", mac)
			}
		}
		if ctx.Err() != nil {
			return
		}
		// Scan even with offline cached remotes so newly synced controllers appear.
		// bluetoothctl's bounded scan removes its own discovery client on exit.
		_, _ = run(ctx, "--timeout", "5", "scan", "on")
		if sleep(ctx, delay) != nil {
			return
		}
		if delay < 15*time.Second {
			delay *= 2
			if delay > 15*time.Second {
				delay = 15 * time.Second
			}
		}
	}
}

func autoConnect(ctx context.Context) { autoConnectLoop(ctx, runBluetoothCommand, sleepContext) }
