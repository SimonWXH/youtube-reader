package main

import (
	"os"
	"path/filepath"
	"testing"
)

// recordRuntimeCalls replaces the two runtime calls the memory tuning makes
// with counters, so a test can assert what the app asked of the runtime
// without capping or collecting the test binary itself. The real calls come
// back when the test ends.
func recordRuntimeCalls(t *testing.T) (limits *[]int64, frees *int) {
	t.Helper()
	previousSet, previousFree := setMemoryLimit, freeOSMemory
	got := []int64{}
	count := 0
	setMemoryLimit = func(limit int64) int64 {
		got = append(got, limit)
		return 0
	}
	freeOSMemory = func() { count++ }
	t.Cleanup(func() {
		setMemoryLimit, freeOSMemory = previousSet, previousFree
	})
	return &got, &count
}

// TestCapMemoryAppliesTheAuditedLimit pins the value the runtime is given and
// the reasoning behind it: the cap has to sit above the picture cache's own
// bitmap budget, which collection cannot evict, or every collection past it
// would free nothing.
func TestCapMemoryAppliesTheAuditedLimit(t *testing.T) {
	limits, _ := recordRuntimeCalls(t)
	previous := capMemory()
	if previous != 0 {
		t.Fatalf("previous limit = %d, want the injected 0", previous)
	}
	const (
		wantLimit          int64 = 192 << 20
		pictureCacheBudget int64 = 128 << 20
	)
	if len(*limits) != 1 || (*limits)[0] != wantLimit {
		t.Fatalf("limits = %v, want one call with %d", *limits, wantLimit)
	}
	if (*limits)[0] <= pictureCacheBudget {
		t.Fatalf("applied limit = %d, must leave the %d picture cache room to live", (*limits)[0], pictureCacheBudget)
	}
}

// TestReleaseIdleMemoryRunsOneCollection pins that a peak's end makes exactly
// one explicit release: not a per-frame cost, and never a silent no-op.
func TestReleaseIdleMemoryRunsOneCollection(t *testing.T) {
	_, frees := recordRuntimeCalls(t)
	releaseIdleMemory()
	if *frees != 1 {
		t.Fatalf("freed %d times, want 1", *frees)
	}
}

// TestRefreshFinishedReleasesMemoryAfterItsPeak pins that the end of a refresh
// is one of the points that hands pages back, whether or not the refresh
// changed anything.
func TestRefreshFinishedReleasesMemoryAfterItsPeak(t *testing.T) {
	_, frees := recordRuntimeCalls(t)
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	w := &Window{app: a}
	w.refreshFinished(RefreshOutcome{}, refreshSnapshot{})
	if *frees != 1 {
		t.Fatalf("freed %d times after a refresh, want 1", *frees)
	}
}

// TestImportOPMLReleasesMemoryAfterItsMerge pins the other peak the audit
// named: a bulk merge ends by handing the pages it freed back to the system.
func TestImportOPMLReleasesMemoryAfterItsMerge(t *testing.T) {
	_, frees := recordRuntimeCalls(t)
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	path := filepath.Join(t.TempDir(), "import.opml")
	body := `<?xml version="1.0"?><opml><body><outline text="Zebra"/></body></opml>`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ImportOPML(path); err != nil {
		t.Fatalf("import: %v", err)
	}
	if *frees != 1 {
		t.Fatalf("freed %d times after an import, want 1", *frees)
	}
}
