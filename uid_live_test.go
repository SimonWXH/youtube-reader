package main

import (
	"os"
	"strconv"
	"testing"
)

// TestUidFromStdlib exercises the production uid() used to render the launch
// agent: it must be the current process's decimal UID and must not depend on
// any external tool, including when PATH is empty.
func TestUidFromStdlib(t *testing.T) {
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", old)

	want := strconv.Itoa(os.Getuid())
	for i := 0; i < 3; i++ {
		if got := uid(); got != want {
			t.Fatalf("uid() = %q, want current process UID %q", got, want)
		}
	}
}
