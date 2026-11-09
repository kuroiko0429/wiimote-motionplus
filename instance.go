//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func instanceLockPath() (string, error) {
	root := os.Getenv("XDG_RUNTIME_DIR")
	if root == "" {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(root, "wiimote-motionplus")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("instance lock directory must be private and owned by this user: %s", dir)
	}
	return filepath.Join(dir, "instance.lock"), nil
}

// Keep the inode after close: unlinking a flock file permits another process to
// lock a new inode while a competing process still holds the old one.
func acquireInstanceLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("instance lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	var stat syscall.Stat_t
	if err = syscall.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Getuid()) || stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		f.Close()
		return nil, fmt.Errorf("unsafe instance lock: %s", path)
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another wiimote-motionplus instance is running (lock %s): %w", path, err)
	}
	return f, nil
}
