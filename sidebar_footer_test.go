package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// sidebarFooterApp is a window with a handful of ungrouped channels, so the
// footer sits under real rows.
func sidebarFooterApp(t *testing.T, channels int) (*App, *Window) {
	t.Helper()
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.Close)
	a.Channels = make([]core.Channel, 0, channels)
	for i := 0; i < channels; i++ {
		channel := core.Channel{ID: fmt.Sprintf("ch-%02d", i), Title: fmt.Sprintf("Channel %02d", i)}
		a.Channels = append(a.Channels, channel)
		a.Sync.Channels[channel.ID] = channel
	}
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.APIKey = "test"
	return a, &Window{app: a, selectedIndex: new(int)}
}

func TestSidebarFooterSitsBelowTheList(t *testing.T) {
	for _, size := range []struct{ w, h int }{{minMainWindowWidth, minMainWindowHeight}, {1280, 808}} {
		for _, channels := range []int{2, 40} {
			_, w := sidebarFooterApp(t, channels)
			tt := ui.NewTester(w.shell, size.w, size.h)
			tt.Frame()
			tokens := mui.NewTokens(nil, false)

			footer, ok := tt.AccessByRole(ui.RoleGroup, "Library actions")
			if !ok {
				t.Fatalf("%dx%d/%d channels: the footer bar is not in the frame", size.w, size.h, channels)
			}
			if footer.Bounds.H != sidebarFooterHeight {
				t.Fatalf("%dx%d/%d channels: footer height = %v pt; want %v",
					size.w, size.h, channels, footer.Bounds.H, sidebarFooterHeight)
			}
			if footer.Bounds.X != 0 || footer.Bounds.W != defaultSidebarWidth {
				t.Fatalf("%dx%d/%d channels: footer at %+v; want the sidebar's width",
					size.w, size.h, channels, footer.Bounds)
			}
			if bottom := footer.Bounds.Y + footer.Bounds.H; bottom != float32(size.h) {
				t.Fatalf("%dx%d/%d channels: footer bottom = %v; want the window's %d",
					size.w, size.h, channels, bottom, size.h)
			}

			plus, ok := tt.AccessByRole(ui.RoleMenuButton, "New group or subscription")
			if !ok {
				t.Fatalf("%dx%d/%d channels: the plus button is not in the frame", size.w, size.h, channels)
			}
			if plus.Bounds.W != sidebarAddSize || plus.Bounds.H != sidebarAddSize {
				t.Fatalf("%dx%d/%d channels: plus button = %vx%v; want %vx%v",
					size.w, size.h, channels, plus.Bounds.W, plus.Bounds.H, sidebarAddSize, sidebarAddSize)
			}
			if plus.Bounds.X != sidebarFooterInset {
				t.Fatalf("%dx%d/%d channels: plus button starts at %v; want the %v inset",
					size.w, size.h, channels, plus.Bounds.X, sidebarFooterInset)
			}
			wantY := footer.Bounds.Y + (sidebarFooterHeight-sidebarAddSize)/2
			if plus.Bounds.Y != wantY {
				t.Fatalf("%dx%d/%d channels: plus button at y %v; want it centred at %v",
					size.w, size.h, channels, plus.Bounds.Y, wantY)
			}

			// Nothing from the list paints in the footer band: with more rows
			// than fit, the list is clipped at the bar, so every pixel of the
			// band is the sidebar surface behind the one plus button.
			image := tt.Image()
			for _, x := range []int{60, 140, int(defaultSidebarWidth) - 20} {
				for y := int(footer.Bounds.Y) + 2; y < int(footer.Bounds.Y+footer.Bounds.H)-1; y++ {
					r, g, b, _ := image.At(x, y).RGBA()
					if absDiff(int(r>>8), int(tokens.Sidebar.R)) > 1 ||
						absDiff(int(g>>8), int(tokens.Sidebar.G)) > 1 ||
						absDiff(int(b>>8), int(tokens.Sidebar.B)) > 1 {
						t.Fatalf("%dx%d/%d channels: the list paints at %d,%d in the footer band",
							size.w, size.h, channels, x, y)
					}
				}
			}
		}
	}
}

func TestSidebarFooterKeepsTheUngroupedSectionAboveIt(t *testing.T) {
	_, w := sidebarFooterApp(t, 2)
	tt := ui.NewTester(w.shell, minMainWindowWidth, minMainWindowHeight)
	tt.Frame()
	footer, ok := tt.AccessByRole(ui.RoleGroup, "Library actions")
	if !ok {
		t.Fatal("the footer bar is not in the frame")
	}
	header, ok := tt.AccessByRole(ui.RoleHeading, "Ungrouped")
	if !ok {
		t.Fatal("the ungrouped section header is not in the frame")
	}
	if header.Bounds.Y+header.Bounds.H > footer.Bounds.Y {
		t.Fatalf("the ungrouped header at %+v runs under the footer at %+v", header.Bounds, footer.Bounds)
	}
}

func TestSidebarPlusMenuOffersTwoCommands(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		_, w := sidebarFooterApp(t, 2)
		tt := ui.NewTester(w.shell, 1280, 808)
		tt.SetLocale(locale)
		tt.Frame()
		if err := tt.Click("New group or subscription"); err != nil {
			t.Fatalf("%s: click the plus button: %v", locale, err)
		}
		menu := tt.Menu()
		want := []string{sidebarMenuCopy(locale).newGroup, sidebarMenuCopy(locale).addSubscription}
		if len(menu) != 2 || menu[0] != want[0] || menu[1] != want[1] {
			t.Fatalf("%s: plus menu = %q; want %q", locale, menu, want)
		}
		if !w.sidebarMenuOpen {
			t.Fatalf("%s: the plus button does not report its menu open", locale)
		}
	}
}

func TestSidebarPlusMenuAddSubscriptionOpensTheDialog(t *testing.T) {
	_, w := sidebarFooterApp(t, 2)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	if err := tt.Click("New group or subscription"); err != nil {
		t.Fatalf("click the plus button: %v", err)
	}
	if err := tt.ChooseMenuItem(sidebarMenuCopy("en").addSubscription); err != nil {
		t.Fatalf("choose Add Subscription…: %v", err)
	}
	tt.Frame()
	if w.sidebarMenuOpen {
		t.Fatal("the plus button stayed pressed after its menu closed")
	}
	if !w.addDialog.open {
		t.Fatal("choosing Add Subscription… did not open the dialog")
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, "Add subscription"); !ok {
		t.Fatal("the dialog panel is not in the frame")
	}
	if w.addDialog.groupID != "" {
		t.Fatalf("the dialog started on group %q; want ungrouped with All videos selected", w.addDialog.groupID)
	}
}

func TestSidebarPlusMenuNewGroupStartsTheInlineRename(t *testing.T) {
	a, w := sidebarFooterApp(t, 2)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	before := len(a.Sync.Groups)
	if err := tt.Click("New group or subscription"); err != nil {
		t.Fatalf("click the plus button: %v", err)
	}
	if err := tt.ChooseMenuItem("New Group"); err != nil {
		t.Fatalf("choose New Group: %v", err)
	}
	tt.Frame()
	if len(a.Sync.Groups) != before+1 {
		t.Fatalf("groups = %d; want the one the menu just added", len(a.Sync.Groups)-before)
	}
	if w.renamingGroupID == "" || w.renamingGroupNew != w.renamingGroupID {
		t.Fatalf("rename state = %q new=%q; want the new group in the inline rename",
			w.renamingGroupID, w.renamingGroupNew)
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, "Rename New Group"); !ok {
		t.Fatal("the inline rename field is not in the frame")
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if len(a.Sync.Groups) != before {
		t.Fatalf("Escape on the new group left %d groups; want the %d it started with",
			len(a.Sync.Groups), before)
	}
	if w.renamingGroupID != "" {
		t.Fatal("Escape did not leave the inline rename")
	}
}

func TestSidebarPlusMenuNewGroupKeepsTheChosenName(t *testing.T) {
	a, w := sidebarFooterApp(t, 2)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	if err := tt.Click("New group or subscription"); err != nil {
		t.Fatalf("click the plus button: %v", err)
	}
	if err := tt.ChooseMenuItem("New Group"); err != nil {
		t.Fatalf("choose New Group: %v", err)
	}
	tt.Frame()
	w.renamingGroupName = "Studio"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if w.renamingGroupID != "" {
		t.Fatal("Enter did not commit the inline rename")
	}
	found := false
	for _, group := range a.Sync.Groups {
		if group.Name == "Studio" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new group is not named Studio: %+v", a.Groups)
	}
}
