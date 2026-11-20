//go:build darwin

package core

import (
	"os"
	"path/filepath"
	"syscall"
)

func flockExclusive(f interface{ Fd() uintptr }, blocking bool) error {
	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

// TryLockDir acquires the local lock without blocking when possible. The
// helper uses it to decide between writing the shared JSON and queueing it.
func TryLockDir(dir string) (*SyncLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".youtube-reader.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := flockExclusive(f, false); err != nil {
		f.Close()
		return nil, err
	}
	return &SyncLock{fd: f, path: path}, nil
}

func flockRelease(f interface{ Fd() uintptr }) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
