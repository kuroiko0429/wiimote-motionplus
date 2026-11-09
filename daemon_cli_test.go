//go:build linux

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonCLIValidationAndCommonLock(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "wiimote-motionplus")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	home := t.TempDir()
	xdg := t.TempDir()
	runtime := t.TempDir()
	path := filepath.Join(xdg, "wiimote-motionplus", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"buttons":{"a":"invalid"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(code int, contains string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdg, "XDG_RUNTIME_DIR="+runtime)
		out, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != code || !strings.Contains(string(out), contains) {
			t.Fatalf("args=%v: %v %s", args, err, out)
		}
	}
	invoke(78, "invalid action", "--daemon", "--config", path)
	invoke(78, "invalid action", "-d")
	invoke(2, "--connect", "--daemon", "--connect")
	if _, err := os.Stat(filepath.Join(runtime, "wiimote-motionplus")); !os.IsNotExist(err) {
		t.Fatal("invalid config created lock")
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	lockPath, err := instanceLockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireInstanceLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	invoke(1, "another wiimote-motionplus instance")
	invoke(1, "another wiimote-motionplus instance", "--daemon")
}

func TestOneShotDoesNotReconnect(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scans, sessions := 0, 0
	deps := daemonDeps{scan: func() ([]deviceSet, error) { scans++; return []deviceSet{{button: "b", gyro: "g"}}, nil }, load: func(string) (map[uint16]string, error) { return defaultBindings(), nil }, now: time.Now, session: func(context.Context, deviceSet, options, map[uint16]string) error { sessions++; return io.EOF }}
	if err := runContext(context.Background(), options{mode: "gyro"}, deps); err != io.EOF || scans != 1 || sessions != 1 {
		t.Fatalf("err=%v scans=%d sessions=%d", err, scans, sessions)
	}
}

func TestDaemonGyroWaitsBeyondOneShotDeadlineWithoutLogSpam(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Unix(1, 0)
	start := now
	scans, logs := 0, 0
	deps := daemonDeps{scan: func() ([]deviceSet, error) { scans++; return []deviceSet{{button: "b", accel: "a"}}, nil }, now: func() time.Time { return now }, log: func(string, ...any) { logs++ }, sleep: func(ctx context.Context, d time.Duration) error {
		if d < 100*time.Millisecond || d > 250*time.Millisecond {
			t.Fatalf("poll: %s", d)
		}
		now = now.Add(d)
		if now.Sub(start) > time.Minute {
			cancel()
		}
		return ctx.Err()
	}, session: func(context.Context, deviceSet, options, map[uint16]string) error {
		t.Fatal("gyro silently used accel")
		return nil
	}}
	if err := daemonLoop(ctx, options{mode: "gyro"}, deps); err != nil {
		t.Fatal(err)
	}
	if scans < 100 || logs != 1 {
		t.Fatalf("scans=%d logs=%d", scans, logs)
	}
}
