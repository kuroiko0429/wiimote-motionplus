//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Replace only the fail-closed systemctl guard, never invoke a real manager.
func mockUserSystemctl(t *testing.T, s installerSandbox) string {
	t.Helper()
	log := filepath.Join(s.scratch, "systemctl.calls")
	script := `#!/bin/sh
case "$*" in
 '--user daemon-reload'|'--user enable --now wiimote-motionplus.service'|'--user restart wiimote-motionplus.service'|'--user is-active --quiet wiimote-motionplus.service') ;;
 *) printf '%s\n' "forbidden systemctl: $*" >> "$BLOCKED";exit 97;;
esac
printf '%s\n' "$*" >> "$CALLS"
if [ "$*" = "${FAIL_COMMAND:-}" ]; then printf 'Failed to connect to user bus\n' >&2;exit 1;fi
`
	guard := filepath.Join(filepath.Dir(s.blocked), "guard", "systemctl")
	if err := os.WriteFile(guard, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return log
}

func verifyUserUnit(t *testing.T, data []byte) {
	t.Helper()
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Log("systemd-analyze unavailable; verifier skipped")
		return
	}
	// Verification must not depend on an installation in the real home directory.
	exe, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	unit := strings.ReplaceAll(string(data), "%h/.local/bin/wiimote-motionplus", exe)
	path := filepath.Join(t.TempDir(), "wiimote-motionplus.service")
	if err := os.WriteFile(path, []byte(unit), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("systemd-analyze", "--user", "verify", path).CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("invalid user unit: %v\n%s", err, out)
	}
}

func TestUserUnitVerifier(t *testing.T) {
	data, err := os.ReadFile("wiimote-motionplus.service")
	if err != nil {
		t.Fatal(err)
	}
	verifyUserUnit(t, data)
}

func TestInstallerUserServiceFailuresAreReported(t *testing.T) {
	commands := []string{"--user daemon-reload", "--user enable --now wiimote-motionplus.service", "--user restart wiimote-motionplus.service", "--user is-active --quiet wiimote-motionplus.service"}
	for index, fail := range commands {
		t.Run(fail, func(t *testing.T) {
			s := newInstallerSandbox(t, false)
			log := mockUserSystemctl(t, s)
			s.env = append(s.env, "BLOCKED="+s.blocked, "CALLS="+log, "FAIL_COMMAND="+fail)
			out, err := s.run(t, "--user-only")
			if err == nil || !strings.Contains(out, "NOT confirmed active") || !strings.Contains(out, "systemctl --user enable --now") || strings.Contains(out, "user service enabled and active:") {
				t.Fatalf("false service success: %v %s", err, out)
			}
			calls, err := os.ReadFile(log)
			if err != nil || string(calls) != strings.Join(commands[:index+1], "\n")+"\n" {
				t.Fatalf("calls after failure: %s %v", calls, err)
			}
			unitDir := filepath.Join(s.home, ".config", "systemd", "user")
			entries, err := os.ReadDir(unitDir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "wiimote-motionplus.service" {
				t.Fatalf("unit staging leaked: %v %v", entries, err)
			}
		})
	}
}

func TestInstallerNoDaemonHasNoUserServiceEffects(t *testing.T) {
	s := newInstallerSandbox(t, false)
	if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
		t.Fatalf("opt out: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(s.home, ".config", "systemd")); !os.IsNotExist(err) {
		t.Fatalf("opt out installed unit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.configPath()), "daemon.env")); !os.IsNotExist(err) {
		t.Fatalf("unexpected daemon.env: %v", err)
	}
}

func TestInstallerEnablesUserDaemonByDefault(t *testing.T) {
	s := newInstallerSandbox(t, true)
	s.xdg = filepath.Join(filepath.Dir(s.xdg), `xdg config % "quoted" \back`)
	log := mockUserSystemctl(t, s)
	s.env = append(s.env, "XDG_CONFIG_HOME="+s.xdg, "BLOCKED="+s.blocked, "CALLS="+log)
	envPath := filepath.Join(filepath.Dir(s.configPath()), "daemon.env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		t.Fatal(err)
	}
	custom := "WIIMOTE_MODE=gyro\nWIIMOTE_GYRO_SENSITIVITY=35\n"
	if err := os.WriteFile(envPath, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := s.run(t, "--user-only"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	} else if !strings.Contains(out, "Press 1+2") || !strings.Contains(out, "journalctl --user") || !strings.Contains(out, "--status") {
		t.Fatalf("missing onboarding: %s", out)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "--user daemon-reload\n--user enable --now wiimote-motionplus.service\n--user restart wiimote-motionplus.service\n--user is-active --quiet wiimote-motionplus.service\n"
	if string(calls) != want {
		t.Fatalf("manager calls: %s", calls)
	}
	unit, err := os.ReadFile(filepath.Join(s.xdg, "systemd", "user", "wiimote-motionplus.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Type=exec", "Restart=on-failure", "RestartSec=2", "RestartPreventExitStatus=2 78", "WantedBy=default.target", `Environment="WIIMOTE_MODE=auto"`, `Environment="WIIMOTE_GYRO_SENSITIVITY=10"`, `--daemon --mode "${WIIMOTE_MODE}" --gyro-sensitivity "${WIIMOTE_GYRO_SENSITIVITY}"`} {
		if !strings.Contains(string(unit), line) {
			t.Fatalf("missing %q:\n%s", line, unit)
		}
	}
	escape := func(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s) }
	if !strings.Contains(string(unit), `Environment="XDG_CONFIG_HOME=`+escape(s.xdg)+`"`) || !strings.Contains(string(unit), `EnvironmentFile=-`+strings.ReplaceAll(envPath, "%", "%%")) {
		t.Fatalf("incorrect config escaping:\n%s", unit)
	}
	if strings.Contains(string(unit), "Requires=bluetooth") || strings.Contains(string(unit), "--connect") {
		t.Fatalf("unsafe user dependencies: %s", unit)
	}
	verifyUserUnit(t, unit)
	got, err := os.ReadFile(envPath)
	if err != nil || string(got) != custom {
		t.Fatalf("daemon.env overwritten: %s %v", got, err)
	}
}
