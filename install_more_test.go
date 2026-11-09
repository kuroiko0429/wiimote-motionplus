//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestInstallerXDGAndReinstallPreservesCustomBytes(t *testing.T) {
	s := newInstallerSandbox(t, true)
	if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	t.Setenv("HOME", s.home)
	t.Setenv("XDG_CONFIG_HOME", s.xdg)
	got, err := loadButtonConfig("")
	if err != nil || !reflect.DeepEqual(got, defaultBindings()) {
		t.Fatalf("XDG loader: %v %v", got, err)
	}
	custom := []byte("{\n  \"buttons\": {\"a\": \"right_click\", \"b\": \"left_click\"}\n}\n")
	if err := os.WriteFile(s.configPath(), custom, 0600); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(s.home, ".local", "bin", "wiimote-mouse")
	if err := os.WriteFile(original, []byte("original program\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
		t.Fatalf("reinstall: %v\n%s", err, out)
	}
	data, err := os.ReadFile(s.configPath())
	if err != nil || string(data) != string(custom) {
		t.Fatalf("custom config replaced: %s %v", data, err)
	}
	got, err = loadButtonConfig("")
	if err != nil || got[btnA] != "right_click" || got[btnB] != "left_click" {
		t.Fatalf("custom behavior lost: %v %v", got, err)
	}
	data, err = os.ReadFile(original)
	if err != nil || string(data) != "original program\n" {
		t.Fatalf("baseline executable changed: %s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(s.home, ".config")); !os.IsNotExist(err) {
		t.Fatalf("ignored XDG root: %v", err)
	}
}

func TestInstallerPreservesConfigSymlinks(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "dangling"}[dangling], func(t *testing.T) {
			s := newInstallerSandbox(t, false)
			if err := os.MkdirAll(filepath.Dir(s.configPath()), 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(s.home, "custom.json")
			if !dangling {
				if err := os.WriteFile(target, []byte("{\"buttons\":{\"home\":\"none\"}}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, s.configPath()); err != nil {
				t.Fatal(err)
			}
			if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			link, err := os.Readlink(s.configPath())
			if err != nil || link != target {
				t.Fatalf("symlink replaced: %q %v", link, err)
			}
			data, err := os.ReadFile(target)
			if dangling {
				if !os.IsNotExist(err) {
					t.Fatalf("dangling target created: %q %v", data, err)
				}
			} else if err != nil || string(data) != "{\"buttons\":{\"home\":\"none\"}}\n" {
				t.Fatalf("symlink target changed: %q %v", data, err)
			}
		})
	}
}

func TestInstallerArgumentValidationHasNoSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"help", []string{"--help"}, true}, {"short-help", []string{"-h"}, true},
		{"unknown", []string{"--unknown"}, false}, {"after-user-only", []string{"--user-only", "--unknown"}, false},
		{"after-help", []string{"--help", "--unknown"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newInstallerSandbox(t, false)
			out, err := s.run(t, tc.args...)
			if (err == nil) != tc.ok {
				t.Fatalf("result: %v %s", err, out)
			}
			if tc.ok && !strings.Contains(out, "--user-only") {
				t.Fatalf("missing usage: %s", out)
			}
			entries, err := os.ReadDir(s.home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("argument parsing changed HOME: %v %v", entries, err)
			}
			entries, err = os.ReadDir(s.scratch)
			if err != nil || len(entries) != 0 {
				t.Fatalf("argument parsing used scratch: %v %v", entries, err)
			}
		})
	}
}

func TestInstallerAtomicallyReplacesRunningExecutable(t *testing.T) {
	s := newInstallerSandbox(t, false)
	exe := filepath.Join(s.home, ".local", "bin", "wiimote-motionplus")
	if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, data, 0755); err != nil {
		t.Fatal(err)
	}
	old := exec.Command(exe, "60")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Process.Kill(); _ = old.Wait() }()
	if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
		t.Fatalf("replace running executable: %v\n%s", err, out)
	}
	if err := old.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("old process disrupted: %v", err)
	}
	if out, err := exec.Command(exe, "--help").CombinedOutput(); err != nil || !strings.Contains(string(out), "-config") {
		t.Fatalf("replacement executable: %v %s", err, out)
	}
}

func TestInstallerMissingDefaultDoesNotPublishEmptyConfig(t *testing.T) {
	s := newInstallerSandbox(t, false)
	checkout := filepath.Join(s.scratch, "checkout")
	if err := os.Mkdir(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := file.Name()
		if name != "install.sh" && name != "go.mod" && (!strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go")) {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checkout, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Run the actual installer in a deliberately incomplete checkout; Go still
	// performs a real build. No host config/default file is removed or modified.
	// Use a separate HOME for the incomplete checkout.
	s2 := newInstallerSandbox(t, false)
	cmd := exec.Command("bash", filepath.Join(checkout, "install.sh"), "--user-only", "--no-daemon")
	cmd.Env = s2.env
	outBytes, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing default reported success: %s", outBytes)
	}
	if _, err := os.Lstat(s2.configPath()); !os.IsNotExist(err) {
		t.Fatalf("published partial config: %v", err)
	}
	for _, dir := range []string{s2.scratch, filepath.Dir(s2.configPath())} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("failed install left staging files: %v %v", entries, err)
		}
	}
}

func TestInstallerUserOnlyRequiresGoWithoutInstallingPackages(t *testing.T) {
	s := newInstallerSandbox(t, false)
	guard := filepath.Join(filepath.Dir(s.blocked), "guard")
	// Restrict PATH so Go is genuinely absent; only the script's read-only
	// prerequisites remain, along with fail-closed system-command guards.
	for _, name := range []string{"dirname", "uname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(guard, name)); err != nil {
			t.Fatal(err)
		}
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "./install.sh", "--user-only", "--no-daemon")
	cmd.Env = append(s.env, "PATH="+guard)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Go is required") {
		t.Fatalf("missing Go: %v %s", err, out)
	}
	if data, err := os.ReadFile(s.blocked); err == nil {
		t.Fatalf("attempted dependency setup: %s", data)
	}
	entries, err := os.ReadDir(s.home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing Go changed HOME: %v %v", entries, err)
	}
}

func TestInstallerConcurrentInvocationsPublishCompleteDefaults(t *testing.T) {
	s := newInstallerSandbox(t, false)
	commands := []*exec.Cmd{exec.Command("bash", "./install.sh", "--user-only", "--no-daemon"), exec.Command("bash", "./install.sh", "--user-only", "--no-daemon")}
	for _, cmd := range commands {
		cmd.Env = s.env
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("concurrent install: %v", err)
		}
	}
	if data, err := os.ReadFile(s.blocked); err == nil {
		t.Fatalf("attempted system setup: %s", data)
	}
	t.Setenv("HOME", s.home)
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := loadButtonConfig("")
	if err != nil || !reflect.DeepEqual(got, defaultBindings()) {
		t.Fatalf("concurrent config: %v %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(s.configPath()))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("config staging leftovers: %v %v", entries, err)
	}
	entries, err = os.ReadDir(s.scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("build staging leftovers: %v %v", entries, err)
	}
}
