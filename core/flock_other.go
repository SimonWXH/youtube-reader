//go:build !darwin

package core

import "os"

func flockExclusive(f *os.File, blocking bool) error { return nil }
func flockRelease(f *os.File) error                  { return nil }
