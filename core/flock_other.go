//go:build !darwin

package core

import (
	"os"
	"path/filepath"
)

func flockExclusive(f *os.File, blocking bool) error { return nil }
func flockRelease(f *os.File) error                  { return nil }

// TryLockDir is a no-op on platforms without flock; writes are atomic.
func TryLockDir(dir string) (*SyncLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".youtube-reader.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &SyncLock{fd: f, path: path}, nil
}
