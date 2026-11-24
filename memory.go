package main

import "runtime/debug"

// memoryLimitBytes is the soft cap on the Go heap. Past it the runtime
// collects instead of growing, which keeps a long browsing session - a refresh
// merging thousands of videos, a screenful of thumbnails decoded - from
// carrying twice its live set between collections, as the default heap goal
// allows.
//
// The audit that asked for this cap started at 128 MB. Measured, 128 MB sits
// below what this app can hold live: netimage, the picture cache every
// thumbnail and avatar comes through, keeps up to 128 MB of decoded bitmaps
// (`mygo@v0.10.0/ui/netimage/netimage.go`), and the reader's own state adds
// about 14 MB, while the running reader already carried 101 MB of Go heap
// after a day of browsing. A cap under the live set is the one setting that
// costs collections and frees nothing, so the cap is raised above that
// ceiling: 192 MB leaves the live set about 35% of headroom and still halves
// the growth the default heap goal allows. Idle, the reader needs 54 MB of
// live heap, so at a fresh launch this cap does not bind at all.
const memoryLimitBytes = 192 << 20

// The runtime calls are variables so a test can watch what the app asks of the
// runtime without capping or collecting the test binary itself.
var (
	setMemoryLimit = debug.SetMemoryLimit
	freeOSMemory   = debug.FreeOSMemory
)

// capMemory applies memoryLimitBytes and returns the limit it replaced, so a
// caller that changes it - a test - can put it back.
func capMemory() int64 {
	return setMemoryLimit(memoryLimitBytes)
}

// releaseIdleMemory hands the pages a peak left behind back to the system:
// memory the collector freed is kept for the next allocation by default, and
// on a long-lived reader that reserve is the difference between the footprint
// of a busy minute and an idle one. It belongs at the end of a job that
// allocates a lot - a refresh, a bulk merge, an import - and never on a frame,
// where its collection would show as a stutter while the reader scrolls.
func releaseIdleMemory() {
	freeOSMemory()
}
