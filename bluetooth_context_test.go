//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutoCommandHonorsCancellation(t *testing.T) {
	guard := t.TempDir()
	if err := os.WriteFile(filepath.Join(guard, "bluetoothctl"), []byte("#!/bin/sh\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", guard+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runBluetoothCommand(ctx, "devices"); err == nil {
		t.Fatal("expected canceled command error")
	}
	if time.Since(start) > time.Second {
		t.Fatal("automatic command ignored cancellation")
	}
}

func TestConnectStartupHonorsCancellation(t *testing.T) {
	guard := t.TempDir()
	if err := os.WriteFile(filepath.Join(guard, "bluetoothctl"), []byte("#!/bin/sh\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", guard+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := connectWiimoteContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Bluetooth startup ignored cancellation")
	}
}
