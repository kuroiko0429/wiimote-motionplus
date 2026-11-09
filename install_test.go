//go:build linux

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type installerSandbox struct {
	home, xdg, scratch, blocked string
	env                         []string
}

func newInstallerSandbox(t *testing.T, xdg bool) installerSandbox {
	t.Helper()
	root := t.TempDir()
	s := installerSandbox{home: filepath.Join(root, "home"), scratch: filepath.Join(root, "scratch"), blocked: filepath.Join(root, "blocked")}
	guard := filepath.Join(root, "guard")
	for _, dir := range []string{s.home, s.scratch, guard} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Any attempt at system setup fails closed, before it can affect the host.
	for _, name := range []string{"sudo", "systemctl", "modinfo", "lsmod", "modprobe", "bluetoothctl", "pacman", "apt", "dnf", "zypper", "udevadm", "usermod"} {
		if err := os.WriteFile(filepath.Join(guard, name), []byte("#!/bin/sh\nprintf '%s\\n' '"+name+"' >> '"+s.blocked+"'\nexit 97\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	s.env = append([]string{}, os.Environ()...)
	// Keep the real build/module caches even though HOME is isolated.
	for _, key := range []string{"GOCACHE", "GOPATH"} {
		out, err := exec.Command("go", "env", key).Output()
		if err != nil {
			t.Fatal(err)
		}
		s.env = append(s.env, key+"="+strings.TrimSpace(string(out)))
	}
	s.env = append(s.env, "HOME="+s.home, "TMPDIR="+s.scratch, "PATH="+guard+":"+os.Getenv("PATH"), "XDG_CONFIG_HOME=")
	if xdg {
		s.xdg = filepath.Join(root, "xdg config")
		s.env = append(s.env, "XDG_CONFIG_HOME="+s.xdg)
	}
	return s
}
func (s installerSandbox) configPath() string {
	root := s.xdg
	if root == "" {
		root = filepath.Join(s.home, ".config")
	}
	return filepath.Join(root, "wiimote-motionplus", "config.json")
}
func (s installerSandbox) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{"./install.sh"}, args...)...)
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	if data, e := os.ReadFile(s.blocked); e == nil {
		t.Fatalf("installer attempted system setup: %s\n%s", data, out)
	}
	return string(out), err
}
func TestInstallerUserOnlyCreatesDefaultConfig(t *testing.T) {
	s := newInstallerSandbox(t, false)
	artifacts := map[string]os.FileInfo{}
	for _, name := range []string{"wiimote-mouse", "wiimote-motionplus"} {
		artifacts[name], _ = os.Stat(name)
	}
	if out, err := s.run(t, "--user-only", "--no-daemon"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Buttons map[string]string `json:"buttons"`
	}
	if !strings.Contains(string(data), "// Wii") || !strings.Contains(string(data), "Available actions") {
		t.Fatalf("installed config is missing documentation: %s", data)
	}
	if err := json.Unmarshal(stripLineComments(data), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Buttons) != len(buttonCodes) {
		t.Fatalf("not all defaults present: %s", data)
	}
	t.Setenv("HOME", s.home)
	t.Setenv("XDG_CONFIG_HOME", "")
	bindings, err := loadButtonConfig("")
	if err != nil || !reflect.DeepEqual(bindings, defaultBindings()) {
		t.Fatalf("generated config differs from defaults: %v %v", bindings, err)
	}
	exe := filepath.Join(s.home, ".local", "bin", "wiimote-motionplus")
	cmd := exec.Command(exe, "--help")
	cmd.Env = s.env
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "-config") {
		t.Fatalf("installed help: %v %s", err, out)
	}
	for name, before := range artifacts {
		after, err := os.Stat(name)
		if before == nil {
			if !os.IsNotExist(err) {
				t.Fatalf("source-tree build artifact %s: %v", name, err)
			}
		} else if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("existing source-tree binary changed: %s", name)
		}
	}
	entries, err := os.ReadDir(s.scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch not cleaned: %v %v", entries, err)
	}
}
