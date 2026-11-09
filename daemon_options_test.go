//go:build linux

package main

import (
	"io"
	"strings"
	"testing"
)

func TestDaemonOptions(t *testing.T) {
	for _, flag := range []string{"-d", "--daemon"} {
		o, err := parseOptions([]string{flag}, io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !o.daemon || o.gyroSensitivity != 10 {
			t.Fatalf("options: %+v", o)
		}
		if _, err := parseOptions([]string{flag, "--connect"}, io.Discard); err == nil || !strings.Contains(err.Error(), "--connect") {
			t.Fatalf("unsafe daemon pairing: %v", err)
		}
	}
}
