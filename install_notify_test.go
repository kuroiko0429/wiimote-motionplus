//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerNotificationDependencyOptionalAndAlreadyPresent(t *testing.T) {
	data, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "# --- optional desktop notifications")
	if start < 0 {
		t.Fatal("missing optional notify-send dependency setup")
	}
	end := strings.Index(text[start:], "# --- allow HID")
	if end < 0 {
		t.Fatal("notification block end missing")
	}
	block := text[start : start+end]
	for _, tc := range []struct{ manager, pkg string }{{"pacman", "libnotify"}, {"apt", "libnotify-bin"}, {"dnf", "libnotify"}, {"zypper", "libnotify-tools"}} {
		for _, present := range []bool{true, false} {
			pre := "set -euo pipefail\nPKG_INSTALL='" + tc.manager + "'\nlog(){ :; }; warn(){ :; }; install_pkg(){ printf '%s' \"$1\"; }\ncommand(){ return 1; }\n"
			want := tc.pkg
			if present {
				pre += "command(){ return 0; }\n"
				want = ""
			}
			out, err := exec.Command("bash", "-c", pre+block).CombinedOutput()
			if err != nil || string(out) != want {
				t.Fatalf("%s present=%v: %v %q", tc.manager, present, err, out)
			}
		}
	}
}
