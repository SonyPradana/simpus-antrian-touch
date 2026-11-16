package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// windowLock is the kiosk window state for one display mode. Kept separate
// from mygo.Window so the rules can be tested without opening a real window.
type windowLock struct {
	fullscreen  bool
	alwaysOnTop bool
	skipTaskbar bool
	resizable   bool
	movable     bool
	minimizable bool
	maximizable bool
	closable    bool
}

// windowLockFor is the only place a display mode is translated into window
// locks. Full mode locks everything; window mode uses the title bar and the
// normal Windows resize frame.
//
// Close is handled through OnClose because DisableClose does not lock the
// native X button. The app can still only exit via the Petugas menu.
func windowLockFor(fullscreen bool) windowLock {
	if fullscreen {
		return windowLock{fullscreen: true, alwaysOnTop: true, skipTaskbar: true}
	}
	return windowLock{resizable: true, movable: true, minimizable: true, maximizable: true}
}

// kioskOptions builds the kiosk window: full screen, always on top, no
// minimize/maximize/close buttons. The window uses a normal Windows frame,
// not frameless: that frame is what lets SetFullScreen toggle back and forth
// without breaking position and size.
func kioskOptions(cfg *Config, view func(*ui.Context)) mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:     "Antrian Pendaftaran",
		Width:     1024,
		Height:    768,
		MinWidth:  900,
		MinHeight: 640,
		Content:   ui.View(view),
	}
	if cfg.Windowed {
		// Development: normal window, closable with the X button.
		return opts
	}

	lock := windowLockFor(true)
	opts.FullScreen = lock.fullscreen
	opts.AlwaysOnTop = lock.alwaysOnTop
	opts.SkipTaskbar = lock.skipTaskbar
	opts.DisableResize = !lock.resizable
	opts.DisableMove = !lock.movable
	opts.DisableClose = !lock.closable
	opts.DisableMinimize = !lock.minimizable
	opts.DisableMaximize = !lock.maximizable
	// F11 calls ToggleFullScreen from the built-in menu. In full mode that
	// means leaving full screen without going through the staff dialog; in
	// window mode the Petugas button already provides that.
	opts.DisableFullScreen = true
	// Theme background color, so there is no white flash before the first frame.
	opts.BackgroundColor = "#f2ecbc"
	return opts
}

// applyWindowMode moves a live window between full screen and a normal
// window. SetFullScreen(false) restores the normal frame first; the mode
// setters run after that, then SetFullScreen(true) saves the intact normal
// frame before entering full screen again.
func applyWindowMode(w *mygo.Window, fullscreen bool) {
	lock := windowLockFor(fullscreen)
	// SetFullScreen moves the window to the monitor corner. MyGo drops
	// that move while movable=false (wmWindowPosChanging → swpNoMove),
	// so the size changes but the position is left behind. Allow movable for
	// now, lock it once the reframe is done.
	w.SetMovable(true)
	if !fullscreen {
		w.SetFullScreen(false)
	}
	w.SetAlwaysOnTop(lock.alwaysOnTop)
	w.SetSkipTaskbar(lock.skipTaskbar)
	w.SetResizable(lock.resizable)
	w.SetMinimizable(lock.minimizable)
	w.SetMaximizable(lock.maximizable)
	w.SetClosable(lock.closable)
	if fullscreen {
		w.SetFullScreen(true)
	}
	w.SetMovable(lock.movable)
}
