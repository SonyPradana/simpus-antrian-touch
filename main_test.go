package main

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// testApp installs the mock and a file-less logger, so tests do not write
// logs to disk.
func testApp() *app {
	cfg := &Config{Mock: true, PoscheckID: "01"}
	return newApp(cfg, nil, newMockAPI())
}

// newTester installs the kiosk screens in a headless tester sized 1024x768.
// Without a window, network calls run inline inside the click.
func newTester(a *app) *ui.Tester {
	return ui.NewTester(a.view, 1024, 768)
}

func clickAll(t *testing.T, tt *ui.Tester, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := tt.Click(name); err != nil {
			t.Fatalf("click %q: %v (texts: %q)", name, err, tt.Texts())
		}
	}
}

// Main flow: existing patient, one doctor, number issued.
func TestRegistrationFlow(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	// Six-digit RM number: the mock treats it as a registered patient.
	clickAll(t, tt, "1", "2", "3", "4", "5", "6", "Cari")

	if !tt.HasText("Pasien Contoh") {
		t.Fatalf("confirmation screen does not show the name, texts: %q", tt.Texts())
	}
	clickAll(t, tt, "Ya, Ini Saya")
	clickAll(t, tt, "Poli Umum")
	clickAll(t, tt, "dr. Contoh Satu")

	if !tt.HasText("Periksa kembali") {
		t.Fatalf("summary screen did not appear, texts: %q", tt.Texts())
	}
	clickAll(t, tt, "Ambil Nomor Antrian")

	if a.screen != screenTicket {
		t.Fatalf("screen %d, want ticket", a.screen)
	}
	if !tt.HasText("A007") {
		t.Errorf("queue number is not shown, texts: %q", tt.Texts())
	}
	// registration mode is used because the patient exists and BPJS is active.
	if a.result == nil || a.result.Mode != "registration" {
		t.Errorf("mode %+v, want registration", a.result)
	}
}

// New patient: no identity, blank mode, single doctor auto-selected.
func TestBlankFlow(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Pasien Baru")
	clickAll(t, tt, "Poli Gigi")

	if a.screen != screenSummary {
		t.Fatalf("screen %d, want summary (a single doctor is chosen automatically)", a.screen)
	}
	clickAll(t, tt, "Ambil Nomor Antrian")

	if a.result == nil || a.result.Mode != "blank" {
		t.Fatalf("mode %+v, want blank", a.result)
	}
	// Blank mode must not carry identity: the server rejects it with 400.
	if a.result.Mode == "blank" && a.result.Patient != nil {
		t.Errorf("blank mode carries a patient: %+v", a.result.Patient)
	}
	if !tt.HasText("B007") {
		t.Errorf("queue number is not shown, texts: %q", tt.Texts())
	}
}

// A patient can back out from the poli screen to the start screen without
// waiting for the timer.
func TestBackFromPoli(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Pasien Baru")
	clickAll(t, tt, "Kembali")

	if a.screen != screenHome {
		t.Errorf("screen %d, want home", a.screen)
	}
}

// The screen must return to start after 120 seconds without touch.
func TestIdleReset(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	if a.screen != screenIdentify {
		t.Fatalf("screen %d, want identify", a.screen)
	}

	// No warning before second 100.
	tt.Advance(99 * time.Second)
	if tt.HasText("Layar kembali ke awal dalam 20 detik") {
		t.Errorf("warning appeared too early, texts: %q", tt.Texts())
	}

	// Second 100: still on screen, countdown visible.
	tt.Advance(time.Second)
	if a.screen != screenIdentify {
		t.Fatalf("reset too early: screen %d", a.screen)
	}
	if !tt.HasText("Layar kembali ke awal dalam 20 detik") {
		t.Errorf("time warning is not shown, texts: %q", tt.Texts())
	}

	// Last second before reset.
	tt.Advance(19 * time.Second)
	if a.screen != screenIdentify {
		t.Fatalf("reset too early: screen %d", a.screen)
	}

	tt.Advance(2 * time.Second)
	if a.screen != screenHome {
		t.Errorf("screen %d after 121 seconds, want home", a.screen)
	}
}

// Touch postpones the reset.
func TestTouchPostponesReset(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	tt.Advance(90 * time.Second)
	clickAll(t, tt, "1")
	tt.Advance(90 * time.Second)

	if a.screen != screenIdentify {
		t.Errorf("screen %d, want identify: a touch at second 90 postpones the reset", a.screen)
	}
	tt.Advance(31 * time.Second)
	if a.screen != screenHome {
		t.Errorf("screen %d after 121 idle seconds, want home", a.screen)
	}
}

// The Cari button is dead while the digits are not valid, so lookup does not
// send an empty or unknown-length identity to the server.
func TestSearchNeedsDigits(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	_ = tt.Click("Cari") // button is dead: the click has no effect
	if a.screen != screenIdentify {
		t.Fatalf("screen %d, want identify: Search must not run without digits", a.screen)
	}

	// Seven digits match no type.
	clickAll(t, tt, "1", "2", "3", "4", "5", "6", "7")
	_ = tt.Click("Cari")
	if a.screen != screenIdentify {
		t.Fatalf("screen %d, want identify: 7 digits are not an identity", a.screen)
	}

	// Six digits: RM number.
	clickAll(t, tt, "Hapus")
	clickAll(t, tt, "Cari")
	if a.screen != screenConfirm {
		t.Fatalf("screen %d, want confirm after 6 digits", a.screen)
	}
}

// ALREADY_REGISTERED (409) is not a failure: the number from the data still
// shows.
func TestAlreadyRegisteredIsSuccess(t *testing.T) {
	a := testApp()
	a.api = &alreadyRegisteredAPI{}
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Pasien Baru", "Poli Gigi", "Ambil Nomor Antrian")

	if a.screen != screenTicket {
		t.Fatalf("screen %d, want ticket: 409 must count as success", a.screen)
	}
	if !tt.HasText("B007") {
		t.Errorf("the old number is not shown, texts: %q", tt.Texts())
	}
	if !tt.HasText("sudah terdaftar") {
		t.Errorf("the already-registered note is not shown, texts: %q", tt.Texts())
	}
	if a.errMsg != "" {
		t.Errorf("errMsg terisi: %q", a.errMsg)
	}
}

// alreadyRegisteredAPI imitates a server that rejects a second registration.
type alreadyRegisteredAPI struct{ mockAPI }

func (m *alreadyRegisteredAPI) Queue(context.Context, QueueBody) (*QueueResult, error) {
	return nil, &APIError{
		Status: 409,
		Code:   "ALREADY_REGISTERED",
		Data:   []byte(`{"antrian":{"kode":"B007"},"poli":{"nama":"Poli Gigi"}}`),
	}
}

// Staff dialog: Batal means stay on screen, Keluar closes the app.
func TestStaffDialogCancelAndQuit(t *testing.T) {
	a := testApp()
	quit := 0
	a.quit = func() { quit++ }
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Petugas")
	if !tt.HasText("Menu Petugas") {
		t.Fatalf("staff dialog did not appear, texts: %q", tt.Texts())
	}

	// Batal: the app stays alive.
	clickAll(t, tt, "Batal")
	if quit != 0 {
		t.Errorf("Cancel closed the app")
	}
	if a.staffOpen {
		t.Errorf("dialog is still open after Cancel")
	}

	// Keluar: quit is called.
	clickAll(t, tt, "Petugas", "Keluar")
	if quit != 1 {
		t.Errorf("quit called %d times, want 1", quit)
	}
}

// One button serves both directions: leave full screen, and enter it again.
// The dialog stays open so the label can be read right after the press.
func TestStaffDialogTogglesWindowMode(t *testing.T) {
	a := testApp()
	var got []bool
	a.setFullscreen = func(v bool) { got = append(got, v) }
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Petugas")
	if !tt.HasText("Tampilan: Layar penuh") {
		t.Fatalf("full-screen status is not shown, texts: %q", tt.Texts())
	}

	clickAll(t, tt, "Pakai jendela biasa")
	if len(got) != 1 || got[0] {
		t.Fatalf("window was not unlocked: %v", got)
	}
	if !a.staffOpen {
		t.Error("dialog closed after the mode switch: staff lose their context")
	}
	if !tt.HasText("Tampilan: Jendela biasa") {
		t.Fatalf("status did not change, texts: %q", tt.Texts())
	}

	// Back to full screen from the same dialog.
	clickAll(t, tt, "Kembali ke layar penuh")
	if len(got) != 2 || !got[1] {
		t.Fatalf("window was not locked again: %v", got)
	}
	if !tt.HasText("Tampilan: Layar penuh") {
		t.Fatalf("status did not revert, texts: %q", tt.Texts())
	}
}

// windowed: true means the window is already normal; the mode switch button
// only confuses because it changes nothing.
func TestStaffDialogHidesWindowModeWhenWindowed(t *testing.T) {
	a := newApp(&Config{Mock: true, Windowed: true}, nil, newMockAPI())
	a.setFullscreen = func(bool) { t.Error("setFullscreen called in windowed mode") }
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Petugas")
	if tt.HasText("Tampilan:") {
		t.Errorf("mode-switch button appeared in windowed mode, texts: %q", tt.Texts())
	}
	if !tt.HasText("Menu Petugas") {
		t.Error("staff dialog is gone")
	}
}

// Staff with a keyboard can use Ctrl+Alt+Q, without touching the screen.
func TestStaffShortcut(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	tt.Key(ui.Ctrl|ui.Alt, ui.KeyQ)
	if !a.staffOpen {
		t.Fatal("Ctrl+Alt+Q did not open the staff dialog")
	}
	if !tt.HasText("Menu Petugas") {
		t.Fatalf("staff dialog did not appear, texts: %q", tt.Texts())
	}
}

// The idle timer must not reset the screen while the staff dialog is open.
func TestStaffDialogStopsIdleReset(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)

	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS", "Petugas")
	if !a.staffOpen {
		t.Fatal("dialog did not open")
	}

	tt.Advance(idleTimeout * 2)
	if a.screen != screenIdentify {
		t.Errorf("screen %d, want identify: the staff dialog must hold the reset", a.screen)
	}
}
