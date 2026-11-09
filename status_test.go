//go:build linux

package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBatteryAndStatusOptions(t *testing.T) {
	o, err := parseOptions([]string{"--status"}, io.Discard)
	if err != nil || !o.status || o.batteryWarning != 20 || o.batteryInterval != time.Minute {
		t.Fatalf("defaults %+v %v", o, err)
	}
	for _, args := range [][]string{{"--battery-warning=-1"}, {"--battery-warning=101"}, {"--battery-interval=4s"}, {"--status", "--connect"}, {"--status", "--daemon"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if _, err := parseOptions([]string{"--battery-warning=0", "--battery-interval=5s"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}
func TestStatusBypassesConfigLockAndSession(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path, err := instanceLockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	var lines []string
	deps := daemonDeps{scan: func() ([]deviceSet, error) { return nil, nil }, load: func(string) (map[uint16]string, error) { t.Fatal("status loaded mouse config"); return nil, nil }, log: func(s string, _ ...any) { lines = append(lines, s) }}
	if err := runContext(context.Background(), options{status: true}, deps); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "No connected Wii Remote") {
		t.Fatalf("status: %v", lines)
	}
}
