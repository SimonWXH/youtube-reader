package main

import (
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/simon/youtube-reader/core"
)

// The main window opens at the board's 1280 x 808, and never below 960 x 640:
// the minimum the window enforces, so no frame that was saved, hand-edited or
// carried over can make the view lay out in less room than it supports.
const (
	defaultMainWindowWidth  = 1280
	defaultMainWindowHeight = 808
	minMainWindowWidth      = 960
	minMainWindowHeight     = 640
)

// mainWindowFrameSaveDelay is how long the window's frame must hold still
// before it is written. Dragging a window or one of its edges raises a move,
// a resize, or both, for every pixel, and every write rewrites the whole
// settings file. The frame is also written at once as the window closes, so a
// change made right before quitting reaches the next launch too.
var mainWindowFrameSaveDelay = 500 * time.Millisecond

// mainWindowFrameAfterFunc schedules the settled frame's write. Tests replace
// it to run the write when they choose instead of after a real delay.
var mainWindowFrameAfterFunc = time.AfterFunc

// mainWindowDisplays lists the displays the main window can open on, so a
// position saved on a display that is gone opens centered instead. Tests
// replace it: MyGo's screen API needs a running app.
var mainWindowDisplays = func() []mygo.Display { return mygo.Screen.Displays() }

// windowFrame is a window's size and position in screen coordinates.
type windowFrame struct {
	X, Y          int
	Width, Height int
}

// positioned reports whether the frame names a position. The pair (0, 0) does
// not: macOS keeps a window's top edge below the menu bar, so that pair only
// comes from a settings file without a position, and it is also what MyGo
// reads as "center this window".
func (f windowFrame) positioned() bool {
	return f.X != 0 || f.Y != 0
}

// frameFromSettings is the frame local settings remember, with the default
// size and no position when none was stored.
func frameFromSettings(s core.Settings) windowFrame {
	f := windowFrame{X: s.WindowX, Y: s.WindowY, Width: s.WindowWidth, Height: s.WindowHeight}
	if f.Width <= 0 {
		f.Width = defaultMainWindowWidth
	}
	if f.Height <= 0 {
		f.Height = defaultMainWindowHeight
	}
	f.Width, f.Height = boundedFrameSize(f.Width, f.Height)
	return f
}

// boundedFrameSize keeps a frame at or above the window's minimum size.
func boundedFrameSize(width, height int) (int, int) {
	return max(width, minMainWindowWidth), max(height, minMainWindowHeight)
}

// mainWindowOptions is the options of the main window: the saved size, and the
// saved position when it still lands on a connected display. Without a saved
// frame, and with one no display can show, the window opens at the default
// size, centered, as MyGo places a window whose position is left at zero.
func mainWindowOptions(settings core.Settings, displays []mygo.Display) mygo.WindowOptions {
	f := frameFromSettings(settings)
	options := mygo.WindowOptions{
		Title:         "YouTube Reader",
		Width:         f.Width,
		Height:        f.Height,
		MinWidth:      minMainWindowWidth,
		MinHeight:     minMainWindowHeight,
		TitleBarStyle: mygo.TitleBarHiddenInset,
	}
	if !f.positioned() {
		return options
	}
	// Without a display to check against, the saved position is the best
	// answer there is: MyGo cannot place a window better than it was left.
	if len(displays) == 0 || frameOnDisplays(f, displays) {
		options.X, options.Y = f.X, f.Y
		return options
	}
	// The frame was saved on a display this Mac no longer has. Opening it
	// centered, no larger than the display it lands on, keeps the window
	// reachable so it can be moved and resized back.
	if work, ok := primaryWorkArea(displays); ok {
		options.Width, options.Height = boundedFrameSize(min(f.Width, work.Width), min(f.Height, work.Height))
	}
	return options
}

// frameOnDisplays reports whether enough of the frame shows on one display to
// grab and drag it: 100 pt of its width and 50 pt of its height, the margin
// MyGo restores a window state with.
func frameOnDisplays(f windowFrame, displays []mygo.Display) bool {
	for _, d := range displays {
		work := d.WorkArea
		shownWidth := min(work.X+work.Width, f.X+f.Width) - max(work.X, f.X)
		shownHeight := min(work.Y+work.Height, f.Y+f.Height) - max(work.Y, f.Y)
		if shownWidth >= min(100, f.Width) && shownHeight >= min(50, f.Height) {
			return true
		}
	}
	return false
}

// primaryWorkArea is the work area of the display with the menu bar, or of the
// first display when none reports being the primary one.
func primaryWorkArea(displays []mygo.Display) (mygo.Rectangle, bool) {
	for _, d := range displays {
		if d.Primary {
			return d.WorkArea, true
		}
	}
	if len(displays) > 0 {
		return displays[0].WorkArea, true
	}
	return mygo.Rectangle{}, false
}

// watchMainWindowFrame remembers the main window's frame across launches. The
// window reports a frame on every resize and move, which settles into one
// write, and the frame is written once more as the window closes: the last
// frame a launch opens with is the one the window was closed at.
func (w *Window) watchMainWindowFrame() {
	if w.window == nil {
		return
	}
	w.frameMemory = newMainWindowFrameMemory(func(f windowFrame) { w.storeMainWindowFrame(f) })
	report := func() { w.reportMainWindowFrame() }
	w.window.OnResize(report)
	w.window.OnMove(report)
	w.window.OnClose(func(*mygo.CloseEvent) {
		w.reportMainWindowFrame()
		w.frameMemory.flush()
	})
}

// reportMainWindowFrame hands the window's current frame to the memory. A
// full screen window reports the frame of a space of its own and a minimized
// one is not on screen at all, so neither frame is one the user chose for the
// window: the frame it had before is the one the next launch opens with. A
// window the system reports without bounds has no frame worth storing either.
func (w *Window) reportMainWindowFrame() {
	if w.window == nil || w.frameMemory == nil {
		return
	}
	if w.window.IsFullScreen() || w.window.IsMinimized() {
		return
	}
	b := w.window.Bounds()
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	w.frameMemory.remember(windowFrame{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height})
}

// storeMainWindowFrame saves the frame as a local UI preference, which the
// next launch opens with. A write that fails is logged: remembering where the
// window was is not worth interrupting the reader over.
func (w *Window) storeMainWindowFrame(f windowFrame) {
	if _, err := w.saveMainWindowFrame(f); err != nil {
		log.Printf("window frame: %v", err)
	}
}

// saveMainWindowFrame stores f in settings.json and reports whether the file
// changed. Writing the frame the file already holds is skipped, so a close
// after a settled resize does not write the whole settings file again.
func (w *Window) saveMainWindowFrame(f windowFrame) (bool, error) {
	width, height := boundedFrameSize(f.Width, f.Height)
	return w.app.updateSettings(func(s *core.Settings) bool {
		if s.WindowWidth == width && s.WindowHeight == height && s.WindowX == f.X && s.WindowY == f.Y {
			return false
		}
		s.WindowWidth, s.WindowHeight = width, height
		s.WindowX, s.WindowY = f.X, f.Y
		return true
	})
}

// mainWindowFrameMemory holds the frame the main window reported last and
// writes it once the window settled. The window reports on the main thread
// while the write runs on the scheduler's goroutine, so the memory guards
// what it holds.
type mainWindowFrameMemory struct {
	mu    sync.Mutex
	frame windowFrame
	have  bool
	timer *time.Timer
	store func(windowFrame)
}

func newMainWindowFrameMemory(store func(windowFrame)) *mainWindowFrameMemory {
	return &mainWindowFrameMemory{store: store}
}

// remember takes the frame the window reports now and schedules its write,
// replacing the frame an earlier report scheduled.
func (m *mainWindowFrameMemory) remember(f windowFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.frame, m.have = f, true
	if m.timer != nil {
		m.timer.Reset(mainWindowFrameSaveDelay)
		return
	}
	m.timer = mainWindowFrameAfterFunc(mainWindowFrameSaveDelay, m.settle)
}

// flush writes the frame the window reports now, without waiting for it to
// settle. A window that closes calls it once.
func (m *mainWindowFrameMemory) flush() {
	m.settle()
}

// settle writes the frame the window settled on, once: a timer that fires
// after a flush finds nothing to write.
func (m *mainWindowFrameMemory) settle() {
	m.mu.Lock()
	f, have := m.frame, m.have
	timer := m.timer
	m.frame, m.have, m.timer = windowFrame{}, false, nil
	m.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if have {
		m.store(f)
	}
}
