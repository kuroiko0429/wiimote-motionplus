//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunContextDaemonReloadInvalidIsFatal(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path := configFile(t, `{"buttons":{"a":"right_click"}}`)
	loads, sessions := 0, 0
	deps := daemonDeps{scan: func() ([]deviceSet, error) { return []deviceSet{{button: "b", gyro: "g"}}, nil }, load: func(path string) (map[uint16]string, error) { loads++; return loadButtonConfig(path) }, now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }, log: func(string, ...any) {}}
	deps.session = func(_ context.Context, _ deviceSet, _ options, b map[uint16]string) error {
		sessions++
		if b[btnA] != "right_click" {
			t.Fatal("mapping lost")
		}
		if err := os.WriteFile(path, []byte(`{"buttons":{"a":"invalid"}}`), 0600); err != nil {
			t.Fatal(err)
		}
		return io.EOF
	}
	err := runContext(context.Background(), options{daemon: true, mode: "gyro", configPath: path}, deps)
	var configErr *configurationError
	if !errors.As(err, &configErr) || loads != 3 || sessions != 1 {
		t.Fatalf("err=%v loads=%d sessions=%d", err, loads, sessions)
	}
}

func TestRunContextValidatesBeforeLockOrScan(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", root)
	path := configFile(t, `{"buttons":{"a":"invalid"}}`)
	deps := daemonDeps{load: loadButtonConfig, scan: func() ([]deviceSet, error) { t.Fatal("hardware scanned"); return nil, nil }}
	err := runContext(context.Background(), options{daemon: true, configPath: path}, deps)
	var configErr *configurationError
	if !errors.As(err, &configErr) {
		t.Fatalf("not configuration failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "wiimote-motionplus")); !os.IsNotExist(err) {
		t.Fatalf("lock before config validation: %v", err)
	}
}

func TestDaemonSignalsWaitingAndActive(t *testing.T) {
	for _, stage := range []string{"waiting", "active"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonSignalHelper$")
			cmd.Env = append(os.Environ(), "WIIMOTE_TEST_SIGNAL="+stage, "XDG_RUNTIME_DIR="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				sent := false
				for scanner.Scan() {
					line := scanner.Text()
					if !sent && ((stage == "waiting" && strings.Contains(line, "Waiting")) || (stage == "active" && strings.Contains(line, "Mode:"))) {
						ready <- true
						sent = true
					}
				}
				if !sent {
					ready <- false
				}
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatalf("helper failed: %s", stderr.String())
				}
			case <-ctx.Done():
				t.Fatal("startup timed out")
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("SIGTERM failed: %v %s", err, stderr.String())
			}
		})
	}
}

func TestDaemonSignalHelper(t *testing.T) {
	stage := os.Getenv("WIIMOTE_TEST_SIGNAL")
	if stage == "" {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	deps := daemonDeps{scan: func() ([]deviceSet, error) {
		if stage == "waiting" {
			return nil, nil
		}
		return []deviceSet{{gyro: "motion", button: "button"}}, nil
	}, load: loadButtonConfig, now: time.Now, sleep: sleepContext, log: func(format string, args ...any) { fmt.Printf(format+"\n", args...) }}
	deps.session = func(ctx context.Context, d deviceSet, o options, b map[uint16]string) error {
		var writers []*os.File
		defer func() {
			for _, f := range writers {
				f.Close()
			}
		}()
		sd := sessionDeps{open: func(string) (*os.File, error) {
			rd, wr, err := os.Pipe()
			writers = append(writers, wr)
			return rd, err
		}, create: func() (*os.File, error) { return os.CreateTemp(t.TempDir(), "ui") }, destroy: func(f *os.File) { f.Close() }, readAbs: func(*os.File, uint16) (int32, error) { return 0, nil }}
		return runSessionWith(ctx, d, o, b, sd)
	}
	if err := runContext(ctx, options{daemon: true, mode: "gyro", gyroSensitivity: 10}, deps); err != nil {
		t.Fatal(err)
	}
}
