//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLockContentionAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.lock")
	first, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireInstanceLock(path); err == nil {
		second.Close()
		t.Fatal("duplicate virtual input allowed")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("stale file blocked restart: %v", err)
	}
	defer next.Close()
}

func TestInstanceLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "lock")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if f, err := acquireInstanceLock(path); err == nil {
		f.Close()
		t.Fatal("followed lock symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("target modified: %s %v", data, err)
	}
}

func TestInstanceLockPathUsesRuntimeOrPrivateCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", root)
	path, err := instanceLockPath()
	if err != nil || path != filepath.Join(root, "wiimote-motionplus", "instance.lock") {
		t.Fatalf("%s %v", path, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	path, err = instanceLockPath()
	if err != nil || !strings.HasPrefix(path, filepath.Join(root, "cache")) {
		t.Fatalf("fallback: %s %v", path, err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("unsafe fallback: %v %v", info, err)
	}
}
