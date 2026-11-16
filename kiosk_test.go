package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A full-screen kiosk uses a normal Windows frame, but locks the controls that
// could let a patient out of the app.
func TestKioskOptionsLockTheWindow(t *testing.T) {
	opts := kioskOptions(&Config{}, func(*ui.Context) {})

	if !opts.AlwaysOnTop {
		t.Error("AlwaysOnTop is off: another window can cover the kiosk")
	}
	if opts.Frameless {
		t.Error("Frameless is on: SetFullScreen needs a normal Windows frame to recover")
	}
	if !opts.FullScreen {
		t.Error("FullScreen is off: the kiosk does not fill the screen")
	}
	if !opts.DisableMinimize || !opts.DisableMaximize {
		t.Error("minimize/maximize buttons are still enabled")
	}
	if !opts.DisableClose {
		t.Error("DisableClose is off; the native X is only guarded by OnClose in main()")
	}
	if !opts.DisableResize || !opts.DisableMove {
		t.Error("the window can still be moved or resized")
	}
	if !opts.DisableFullScreen {
		t.Error("DisableFullScreen is off: F11 can leave full screen")
	}
	if !opts.SkipTaskbar {
		t.Error("the kiosk still shows up in the taskbar")
	}
}

// windowed: true uses a normal window so development is not held hostage.
func TestWindowedOptionForDevelopment(t *testing.T) {
	opts := kioskOptions(&Config{Windowed: true}, func(*ui.Context) {})

	if opts.FullScreen || opts.Frameless || opts.DisableClose || opts.AlwaysOnTop {
		t.Errorf("windowed still locks the window: %+v", opts)
	}
	if opts.Width != 1024 || opts.Height != 768 {
		t.Errorf("size %dx%d, want 1024x768", opts.Width, opts.Height)
	}
}

// Display mode is translated into window locks in one place. If full mode does
// not lock, patients can get out on their own; if window mode does not unlock,
// staff cannot move a window they already shrank.
func TestWindowLockFor(t *testing.T) {
	full := windowLockFor(true)
	if !full.fullscreen || !full.alwaysOnTop || !full.skipTaskbar {
		t.Errorf("full mode is not locked: %+v", full)
	}
	if full.resizable || full.movable || full.minimizable || full.maximizable || full.closable {
		t.Errorf("full mode leaves an escape route: %+v", full)
	}

	win := windowLockFor(false)
	if win.fullscreen || win.alwaysOnTop || win.skipTaskbar {
		t.Errorf("window mode still covers the screen: %+v", win)
	}
	if !win.resizable || !win.movable || !win.minimizable || !win.maximizable {
		t.Errorf("window mode does not use normal controls: %+v", win)
	}
	if win.closable {
		t.Errorf("window mode allows close without the Petugas path: %+v", win)
	}
}

// An exit must always exist and be visible. The five-tap gesture on an
// invisible box was tried and failed: staff never found it.
func TestStaffButtonIsVisible(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	if !tt.HasText("Petugas") {
		t.Fatalf("Petugas button is not shown, texts: %q", tt.Texts())
	}
	clickAll(t, tt, "Petugas")
	if !a.staffOpen {
		t.Fatal("Petugas button did not open the dialog")
	}
}

// The Petugas button must not cover screen action buttons. The old gesture
// box (140x140 in the corner) covered "Ya, Ini Saya" — that is what made the
// kiosk unusable.
func TestStaffButtonDoesNotCoverActions(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS", "1", "2", "3", "4", "5", "6", "Cari")
	if a.errMsg != "" {
		t.Fatalf("search failed: %q", a.errMsg)
	}

	staff, ok := tt.Find("Petugas")
	if !ok {
		t.Fatal("Petugas button is missing")
	}
	for _, name := range []string{"Ya, Ini Saya", "Bukan Saya"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("button %q is missing", name)
		}
		if overlap(staff, r) {
			t.Errorf("Petugas button %+v overlaps %q %+v", staff, name, r)
		}
	}
}

// overlap reports whether two boxes intersect.
func overlap(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
