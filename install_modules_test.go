//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerLoadedUinputDoesNotReloadWithLargeModuleList(t *testing.T) {
	data, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "if ! lsmod |")
	if start < 0 {
		t.Fatal("module check missing")
	}
	end := strings.Index(text[start:], "\nif [ ! -f /etc/modules-load.d/uinput.conf ]")
	if end < 0 {
		t.Fatal("module block end missing")
	}
	script := `set -euo pipefail
lsmod() { printf 'uinput 20480 0\n'; for ((i=0;i<10000;i++)); do printf 'another_module 12345 1\n'; done; }
sudo() { printf 'UNEXPECTED_SUDO\n'; return 1; }
log() { printf '%s\n' "$1"; }
warn() { printf '%s\n' "$1"; }
` + text[start:start+end]
	out, err := exec.Command("bash", "-c", script).CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("loaded module triggered reload: %v %s", err, out)
	}
}
