//go:build darwin

package core

import "syscall"

func flockExclusive(f interface{ Fd() uintptr }, blocking bool) error {
	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

func flockRelease(f interface{ Fd() uintptr }) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
