package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/simon/youtube-reader/core"
)

// testDisplay is a display whose work area starts at (x, y).
func testDisplay(x, y, width, height int, primary bool) mygo.Display {
	return mygo.Display{
		WorkArea: mygo.Rectangle{X: x, Y: y, Width: width, Height: height},
		Primary:  primary,
	}
}

// frameScheduler replaces the settled frame's scheduler for one test, so the
// write runs when the test says it does rather than after a real delay.
type frameScheduler struct {
	delay time.Duration
	job   func()
	armed int
	timer *time.Timer
}

func stubFrameScheduler(t *testing.T) *frameScheduler {
	t.Helper()
	s := &frameScheduler{timer: time.NewTimer(time.Hour)}
	previousAfter, previousDelay := mainWindowFrameAfterFunc, mainWindowFrameSaveDelay
	mainWindowFrameSaveDelay = 750 * time.Millisecond
	mainWindowFrameAfterFunc = func(d time.Duration, fn func()) *time.Timer {
		s.delay, s.job = d, fn
		s.armed++
		return s.timer
	}
	t.Cleanup(func() {
		mainWindowFrameAfterFunc, mainWindowFrameSaveDelay = previousAfter, previousDelay
		s.timer.Stop()
	})
	return s
}

func TestMainWindowOptionsDefaults(t *testing.T) {
	options := mainWindowOptions(core.Settings{}, nil)
	if options.Width != defaultMainWindowWidth || options.Height != defaultMainWindowHeight {
		t.Fatalf("first launch opens %dx%d; want %dx%d",
			options.Width, options.Height, defaultMainWindowWidth, defaultMainWindowHeight)
	}
	if options.Width != 1280 || options.Height != 808 {
		t.Fatalf("the board's default window is 1280x808; got %dx%d", options.Width, options.Height)
	}
	if options.MinWidth != 960 || options.MinHeight != 640 {
		t.Fatalf("minimum size = %dx%d; want 960x640", options.MinWidth, options.MinHeight)
	}
	if options.X != 0 || options.Y != 0 {
		t.Fatalf("first launch asks for position (%d, %d); want centered at (0, 0)", options.X, options.Y)
	}
	if options.Title != "YouTube Reader" || options.TitleBarStyle != mygo.TitleBarHiddenInset {
		t.Fatalf("window chrome = %q, %v", options.Title, options.TitleBarStyle)
	}
}

func TestMainWindowOptionsRestoresSavedFrame(t *testing.T) {
	settings := core.Settings{WindowWidth: 1440, WindowHeight: 900, WindowX: 240, WindowY: 120}
	options := mainWindowOptions(settings, []mygo.Display{testDisplay(0, 0, 2560, 1440, true)})
	if options.Width != 1440 || options.Height != 900 {
		t.Fatalf("restored size = %dx%d; want 1440x900", options.Width, options.Height)
	}
	if options.X != 240 || options.Y != 120 {
		t.Fatalf("restored position = (%d, %d); want (240, 120)", options.X, options.Y)
	}
}

func TestMainWindowOptionsKeepsSavedPositionWhenNoDisplayAnswers(t *testing.T) {
	settings := core.Settings{WindowWidth: 1200, WindowHeight: 760, WindowX: -900, WindowY: 40}
	options := mainWindowOptions(settings, nil)
	if options.X != -900 || options.Y != 40 {
		t.Fatalf("position without displays = (%d, %d); want the saved (-900, 40)", options.X, options.Y)
	}
	if options.Width != 1200 || options.Height != 760 {
		t.Fatalf("size without displays = %dx%d; want 1200x760", options.Width, options.Height)
	}
}

func TestMainWindowOptionsUsesDefaultSizeWhenOnlyPositionWasSaved(t *testing.T) {
	settings := core.Settings{WindowX: 300, WindowY: 200}
	options := mainWindowOptions(settings, []mygo.Display{testDisplay(0, 0, 1920, 1080, true)})
	if options.Width != defaultMainWindowWidth || options.Height != defaultMainWindowHeight {
		t.Fatalf("size = %dx%d; want the default %dx%d",
			options.Width, options.Height, defaultMainWindowWidth, defaultMainWindowHeight)
	}
	if options.X != 300 || options.Y != 200 {
		t.Fatalf("position = (%d, %d); want the saved (300, 200)", options.X, options.Y)
	}
}

func TestMainWindowOptionsBoundedByMinimumSize(t *testing.T) {
	settings := core.Settings{WindowWidth: 320, WindowHeight: 200, WindowX: 10, WindowY: 20}
	options := mainWindowOptions(settings, []mygo.Display{testDisplay(0, 0, 1920, 1080, true)})
	if options.Width != minMainWindowWidth || options.Height != minMainWindowHeight {
		t.Fatalf("size = %dx%d; want the minimum %dx%d",
			options.Width, options.Height, minMainWindowWidth, minMainWindowHeight)
	}
	if options.X != 10 || options.Y != 20 {
		t.Fatalf("position = (%d, %d); want (10, 20)", options.X, options.Y)
	}
}

func TestMainWindowOptionsNeverBelowMinimumOnASmallDisplay(t *testing.T) {
	settings := core.Settings{WindowWidth: 1600, WindowHeight: 900, WindowX: 5000, WindowY: 5000}
	options := mainWindowOptions(settings, []mygo.Display{testDisplay(0, 0, 800, 600, true)})
	if options.Width < minMainWindowWidth || options.Height < minMainWindowHeight {
		t.Fatalf("size on a small display = %dx%d; want at least %dx%d",
			options.Width, options.Height, minMainWindowWidth, minMainWindowHeight)
	}
	if options.X != 0 || options.Y != 0 {
		t.Fatalf("position = (%d, %d); want centered", options.X, options.Y)
	}
}

func TestMainWindowOptionsRestoresFrameOnSecondaryDisplay(t *testing.T) {
	displays := []mygo.Display{
		testDisplay(0, 0, 1920, 1080, true),
		testDisplay(-1920, 0, 1920, 1080, false),
	}
	settings := core.Settings{WindowWidth: 1200, WindowHeight: 800, WindowX: -1500, WindowY: 100}
	options := mainWindowOptions(settings, displays)
	if options.X != -1500 || options.Y != 100 {
		t.Fatalf("position = (%d, %d); want the saved (-1500, 100)", options.X, options.Y)
	}
}

func TestMainWindowOptionsCentersFrameAwayFromEveryDisplay(t *testing.T) {
	displays := []mygo.Display{testDisplay(0, 25, 1920, 1050, true)}
	settings := core.Settings{WindowWidth: 2000, WindowHeight: 1400, WindowX: 4000, WindowY: 3000}
	options := mainWindowOptions(settings, displays)
	if options.X != 0 || options.Y != 0 {
		t.Fatalf("unreachable frame opens at (%d, %d); want centered", options.X, options.Y)
	}
	if options.Width != 1920 || options.Height != 1050 {
		t.Fatalf("unreachable frame opens %dx%d; want it fitted to the work area 1920x1050",
			options.Width, options.Height)
	}
}

func TestMainWindowOptionsKeepsBarelyReachableFrame(t *testing.T) {
	displays := []mygo.Display{testDisplay(0, 0, 1920, 1080, true)}
	// 100 pt of the width and 50 pt of the height still show, the margin a
	// window needs to be grabbed and dragged back.
	settings := core.Settings{WindowWidth: 1280, WindowHeight: 808, WindowX: 1820, WindowY: 1030}
	options := mainWindowOptions(settings, displays)
	if options.X != 1820 || options.Y != 1030 {
		t.Fatalf("reachable frame opens at (%d, %d); want the saved (1820, 1030)", options.X, options.Y)
	}

	// One point further and off the display is no frame to open at.
	settings.WindowX, settings.WindowY = 1821, 1031
	options = mainWindowOptions(settings, displays)
	if options.X != 0 || options.Y != 0 {
		t.Fatalf("unreachable frame opens at (%d, %d); want centered", options.X, options.Y)
	}
}

func TestFrameFromSettings(t *testing.T) {
	cases := []struct {
		name       string
		settings   core.Settings
		width      int
		height     int
		positioned bool
	}{
		{
			name:   "nothing saved",
			width:  defaultMainWindowWidth,
			height: defaultMainWindowHeight,
		},
		{
			name:     "default size with a saved position",
			settings: core.Settings{WindowX: 60, WindowY: 80},
			width:    defaultMainWindowWidth,
			height:   defaultMainWindowHeight, positioned: true,
		},
		{
			name:     "negative size falls back to the default",
			settings: core.Settings{WindowWidth: -20, WindowHeight: -40, WindowX: 60, WindowY: 80},
			width:    defaultMainWindowWidth,
			height:   defaultMainWindowHeight, positioned: true,
		},
		{
			name:     "a stored size below the minimum is raised to it",
			settings: core.Settings{WindowWidth: 800, WindowHeight: 500, WindowX: 60, WindowY: 80},
			width:    minMainWindowWidth,
			height:   minMainWindowHeight, positioned: true,
		},
		{
			name:     "x alone is a position",
			settings: core.Settings{WindowWidth: 1200, WindowHeight: 800, WindowX: 120},
			width:    1200, height: 800, positioned: true,
		},
		{
			name:     "the zero pair is no position",
			settings: core.Settings{WindowWidth: 1200, WindowHeight: 800},
			width:    1200, height: 800,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := frameFromSettings(tc.settings)
			if frame.Width != tc.width || frame.Height != tc.height {
				t.Fatalf("frame size = %dx%d; want %dx%d", frame.Width, frame.Height, tc.width, tc.height)
			}
			if got := frame.positioned(); got != tc.positioned {
				t.Fatalf("frame positioned = %v; want %v", got, tc.positioned)
			}
		})
	}
}

func TestMainWindowFrameMemoryWritesSettledFrame(t *testing.T) {
	scheduler := stubFrameScheduler(t)
	var stored []windowFrame
	memory := newMainWindowFrameMemory(func(f windowFrame) { stored = append(stored, f) })

	memory.remember(windowFrame{X: 10, Y: 20, Width: 1200, Height: 800})
	settled := windowFrame{X: 40, Y: 60, Width: 1400, Height: 900}
	memory.remember(settled)

	if scheduler.armed != 1 {
		t.Fatalf("two reports armed %d writes; want one write after the frame settles", scheduler.armed)
	}
	if scheduler.delay != 750*time.Millisecond {
		t.Fatalf("the write was scheduled after %v; want the settle delay 750ms", scheduler.delay)
	}
	if len(stored) != 0 {
		t.Fatalf("the frame was written before it settled: %+v", stored)
	}

	scheduler.job()
	if len(stored) != 1 || stored[0] != settled {
		t.Fatalf("settled writes = %+v; want exactly the last frame %+v", stored, settled)
	}

	// A timer that fires after the write has nothing left to write.
	scheduler.job()
	if len(stored) != 1 {
		t.Fatalf("a late timer wrote %+v again", stored)
	}
}

func TestMainWindowFrameMemoryFlushWritesAtOnce(t *testing.T) {
	scheduler := stubFrameScheduler(t)
	var stored []windowFrame
	memory := newMainWindowFrameMemory(func(f windowFrame) { stored = append(stored, f) })

	frame := windowFrame{X: -1200, Y: 60, Width: 1280, Height: 808}
	memory.remember(frame)
	memory.flush()

	if len(stored) != 1 || stored[0] != frame {
		t.Fatalf("flush wrote %+v; want the reported frame %+v", stored, frame)
	}

	scheduler.job()
	if len(stored) != 1 {
		t.Fatalf("the timer wrote %+v after the flush", stored)
	}
}

func TestMainWindowFrameRoundTripThroughSettings(t *testing.T) {
	dir := t.TempDir()
	app := newTestApp(t, dir)
	defer app.Close()
	w := &Window{app: app}

	saved := windowFrame{X: 240, Y: 120, Width: 1440, Height: 900}
	changed, err := w.saveMainWindowFrame(saved)
	if err != nil {
		t.Fatalf("save the frame: %v", err)
	}
	if !changed {
		t.Fatal("the first frame was not written")
	}

	// A second launch reads the frame back from settings.json.
	settings := loadSettingsFile(t, dir)
	if settings.WindowWidth != 1440 || settings.WindowHeight != 900 {
		t.Fatalf("stored size = %dx%d; want 1440x900", settings.WindowWidth, settings.WindowHeight)
	}
	if settings.WindowX != 240 || settings.WindowY != 120 {
		t.Fatalf("stored position = (%d, %d); want (240, 120)", settings.WindowX, settings.WindowY)
	}
	options := mainWindowOptions(settings, []mygo.Display{testDisplay(0, 0, 2560, 1440, true)})
	if options.Width != saved.Width || options.Height != saved.Height {
		t.Fatalf("restored size = %dx%d; want %dx%d", options.Width, options.Height, saved.Width, saved.Height)
	}
	if options.X != saved.X || options.Y != saved.Y {
		t.Fatalf("restored position = (%d, %d); want (%d, %d)", options.X, options.Y, saved.X, saved.Y)
	}

	// The keys in the file are the ones the fields name, so an older build
	// that does not know them still reads the rest of its settings.
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	for _, key := range []string{"windowWidth", "windowHeight", "windowX", "windowY"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("settings.json has no %q: %s", key, data)
		}
	}

	// The frame that is already stored is not written again.
	changed, err = w.saveMainWindowFrame(saved)
	if err != nil {
		t.Fatalf("save the frame again: %v", err)
	}
	if changed {
		t.Fatal("the frame the file already holds was written again")
	}
}

func TestSaveMainWindowFrameRaisesSmallFrameToTheMinimum(t *testing.T) {
	dir := t.TempDir()
	app := newTestApp(t, dir)
	defer app.Close()
	w := &Window{app: app}

	if _, err := w.saveMainWindowFrame(windowFrame{X: 5, Y: 6, Width: 320, Height: 200}); err != nil {
		t.Fatalf("save the frame: %v", err)
	}
	settings := loadSettingsFile(t, dir)
	if settings.WindowWidth != minMainWindowWidth || settings.WindowHeight != minMainWindowHeight {
		t.Fatalf("stored size = %dx%d; want the minimum %dx%d",
			settings.WindowWidth, settings.WindowHeight, minMainWindowWidth, minMainWindowHeight)
	}
	if settings.WindowX != 5 || settings.WindowY != 6 {
		t.Fatalf("stored position = (%d, %d); want (5, 6)", settings.WindowX, settings.WindowY)
	}
}
