package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// idleTimeout per the PRD: the screen returns to start after 120 seconds
	// without touch, with a warning starting at second 100.
	idleTimeout = 120 * time.Second
	idleWarn    = idleTimeout - 20*time.Second

	requestTimeout = 20 * time.Second
)

type screen int

const (
	screenHome screen = iota
	screenIdentify
	screenConfirm
	screenPoli
	screenDoctor
	screenSummary
	screenTicket
)

// apiClient is the part of the server used by the kiosk. Separated from
// *Client so screen flows can be tested with a mock and no network.
type apiClient interface {
	Lookup(ctx context.Context, identType, value string, pcare bool) (*LookupResult, error)
	Poli(ctx context.Context) ([]Poli, error)
	Doctors(ctx context.Context, poliID string) (*DoctorList, error)
	Queue(ctx context.Context, body QueueBody) (*QueueResult, error)
}

// app is the kiosk state. Every field is touched from the UI thread; network
// call results come in via Window.Update.
type app struct {
	cfg   *Config
	log   *Logger
	api   apiClient
	theme *ui.Theme
	// update writes state from another goroutine onto the UI thread. Nil in
	// tests, where work runs inline so frame order is deterministic.
	update func(func())

	screen screen
	// digits is the identity box content as-is. The type is not stored:
	// the digit count decides, via ParseIdentifier.
	digits string
	// blank is chosen for a new patient: queue mode `blank`, no identity.
	blank bool

	lookupRes *LookupResult
	polis     []Poli
	poli      *Poli
	doctors   []Doctor
	doctor    *Doctor
	result    *QueueResult

	busy   bool
	errMsg string

	// gen rises on every reset, so late server answers (after the patient
	// left) are discarded.
	gen int

	touchedAt time.Time
	// now is the time of the last frame. Stored so the remaining time on
	// screen uses the same clock as the timer, including under test.
	now time.Time

	// staffOpen shows the staff dialog. While open, the idle timer stops
	// so staff are not thrown back to the start screen while working.
	staffOpen bool
	// fullscreen is the current display mode: true means the kiosk covers
	// the screen. Staff can turn it off from the Petugas dialog, for example to
	// open another app without killing the kiosk. Not saved to
	// config.json: kiosk mode must be the starting state every time the app
	// runs.
	fullscreen bool
	// setFullscreen applies the display mode to the MyGo window. Filled in by
	// main() with the real window; nil in tests, where nothing can be moved.
	setFullscreen func(bool)
	// prn prints the ticket after a number is issued. Nil when printing is
	// off, in tests, or when no printer was configured. Printing is best
	// effort, so a nil printer is a no-op rather than an error.
	prn printer
	// printNote is the last printer test result. Printing is silent and never
	// blocks the kiosk, so this is the only place staff can find out that a
	// printer is unplugged or out of paper.
	printNote string
	// quit closes the app. Filled in by main() with the MyGo Window/App; nil
	// in tests, where closing the app means nothing.
	quit func()
}

func newApp(cfg *Config, lg *Logger, api apiClient) *app {
	// windowed: true means the window is already normal; do not show a mode
	// switch button that changes nothing.
	return &app{cfg: cfg, log: lg, api: api, theme: LotusTheme(), fullscreen: !cfg.Windowed}
}

func (a *app) view(c *ui.Context) {
	c.SetTheme(a.theme)
	a.tick(c)
	t := c.Theme()

	// Escape hatch for staff who plug in a keyboard: Ctrl+Alt+Q.
	c.OnWindowShortcut(ui.Ctrl|ui.Alt, ui.KeyQ, func() {
		a.log.Event("pintasan petugas: Ctrl+Alt+Q")
		a.staffOpen = true
	})

	ui.Column(c).Fill().Padding(28).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Antrian Pendaftaran").FontSize(26).Bold()
			ui.Spacer(c)
			ui.Text(c, a.hint()).FontSize(16).TextColor(t.TextMuted)
		})
		ui.Divider(c)

		ui.Column(c).Grow(1).Gap(18).Children(func() {
			a.body(c)
		})

		if a.errMsg != "" {
			ui.Row(c).Gap(16).Padding(16).Radius(12).
				Background(t.Danger.Alpha(0.12)).Children(func() {
				ui.Text(c, a.errMsg).FontSize(18).TextColor(t.Danger).Grow(1)
				if ui.Button(c.Key("dismiss"), "Tutup").FontSize(16).Clicked() {
					a.touch()
					a.errMsg = ""
				}
			})
		}
		a.footer(c)
	})
	a.staffDialog(c)
}

// staffDialog is the staff menu: switch display mode, then exit. A closed
// kiosk cannot be used until staff run it again, so one extra tap is cheap.
func (a *app) staffDialog(c *ui.Context) {
	ui.Modal(c, &a.staffOpen, func() {
		ui.Column(c).Gap(16).Width(560).Children(func() {
			ui.Text(c, "Menu Petugas").FontSize(26).Bold()

			// Mode switch on top, exit at the bottom: staff who open this dialog
			// far more often want to shrink the window than to close the
			// kiosk, and a destructive button should not be the one nearest the
			// thumb.
			a.windowModeRow(c)
			a.printerRow(c)
			ui.Divider(c)

			ui.Text(c, "Kiosk tidak bisa dipakai pasien sampai aplikasi dijalankan lagi.").
				FontSize(18).TextColor(c.Theme().TextMuted)
			ui.Row(c).Gap(16).Justify(ui.End).Children(func() {
				// Highlight "Batal": an accidental tap on the primary button
				// must mean staying on screen, not closing the kiosk.
				if ui.PrimaryButton(c.Key("staff-cancel"), "Batal").FontSize(20).Height(64).Clicked() {
					a.staffOpen = false
				}
				if ui.Button(c.Key("staff-quit"), "Keluar").FontSize(20).Height(64).
					TextColor(c.Theme().Danger).Clicked() {
					if a.quit != nil {
						a.quit()
					}
				}
			})
		})
	})
}

// windowModeRow provides a way out of full screen, and back in: one
// button whose label follows the current mode, so staff do not have to tell
// two buttons apart.
//
// The dialog deliberately stays open after a press. The label changes
// immediately, so staff see the press landed and can flip it back right away
// if it was the wrong one.
func (a *app) windowModeRow(c *ui.Context) {
	// With "windowed": true the window is already normal, so the button is
	// pointless. In tests a.setFullscreen is nil: there is no real window to
	// move, and a button with no effect is worse than none.
	if a.cfg.Windowed || a.setFullscreen == nil {
		return
	}
	label, action := "Layar penuh", "Pakai jendela biasa"
	if !a.fullscreen {
		label, action = "Jendela biasa", "Kembali ke layar penuh"
	}
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Tampilan: "+label).FontSize(18).TextColor(c.Theme().TextMuted).Grow(1)
		if ui.Button(c.Key("staff-window-mode"), action).FontSize(18).Height(56).Clicked() {
			a.setFullscreenMode(!a.fullscreen)
		}
	})
}

// setFullscreenMode changes the display mode through one door, so app
// state and the MyGo window never disagree.
func (a *app) setFullscreenMode(fullscreen bool) {
	a.fullscreen = fullscreen
	if a.setFullscreen != nil {
		a.setFullscreen(fullscreen)
	}
	a.log.Event("tampilan: layar penuh=%t", fullscreen)
}

// printerRow shows which printer is configured and prints a sample ticket on
// demand. Printing is deliberately silent, so a printer that has been
// unplugged would otherwise go unnoticed until a patient complains.
func (a *app) printerRow(c *ui.Context) {
	if a.prn == nil {
		ui.Text(c, "Printer: mati, nomor hanya tampil di layar").
			FontSize(18).TextColor(c.Theme().TextMuted)
		return
	}
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Printer: "+a.prn.String()).
			FontSize(18).TextColor(c.Theme().TextMuted).Grow(1)
		if ui.Button(c.Key("staff-print-test"), "Tes cetak").FontSize(18).Height(56).Clicked() {
			a.testPrint()
		}
	})
	if a.printNote != "" {
		ui.Text(c, a.printNote).FontSize(16).TextColor(c.Theme().TextMuted)
	}
}

// testPrint sends a sample ticket. The result is shown in the dialog and
// logged, never as a modal: staff testing a printer should not have to dismiss
// anything to get back to the queue.
func (a *app) testPrint() {
	job := encodeReceipt(sampleReceipt(a.now), a.cfg.Printer.printerWidth())
	a.printNote = "Mengirim ke " + a.prn.String() + "..."
	a.async(func() func() {
		err := a.prn.Print(job, 1)
		return func() {
			if err != nil {
				a.printNote = "Gagal: " + err.Error()
				a.log.Error("tes cetak gagal: %v", err)
				return
			}
			a.printNote = "Terkirim ke " + a.prn.String()
			a.log.Event("tes cetak terkirim ke %s", a.prn)
		}
	})
}

// tick maintains the 120-second timer. Every frame still far from the limit
// requests one more frame, so the countdown runs even while the patient is idle.
func (a *app) tick(c *ui.Context) {
	now := c.Now()
	a.now = now
	if a.touchedAt.IsZero() {
		a.touchedAt = now
	}
	// While the staff menu is open, the screen must not reset itself: staff
	// are working, it is not a patient who left.
	if a.staffOpen {
		a.touchedAt = now
		return
	}
	if now.Sub(a.touchedAt) >= idleTimeout {
		if a.screen != screenHome {
			a.log.Event("reset otomatis setelah %s diam di layar %d", idleTimeout, a.screen)
			a.reset()
		}
		a.touchedAt = now
		return
	}
	if a.screen != screenHome {
		c.After(time.Second)
	}
}

// touch records the last touch. The time comes from the frame being built,
// not time.Now(), so it matches the timer clock above.
func (a *app) touch() { a.touchedAt = a.now }

func (a *app) reset() {
	a.gen++
	a.screen = screenHome
	a.digits = ""
	a.blank = false
	a.lookupRes = nil
	a.polis = nil
	a.poli = nil
	a.doctors = nil
	a.doctor = nil
	a.result = nil
	a.busy = false
	a.errMsg = ""
}

// async runs work in a goroutine then applies the result on the UI thread.
// Without a window (tests) work runs inline, so clicks and state changes stay
// in the order the test reads them.
func (a *app) async(work func() func()) {
	if a.update == nil {
		if done := work(); done != nil {
			done()
		}
		return
	}
	go func() {
		done := work()
		if done == nil {
			return
		}
		a.update(done)
	}()
}

// wait delays a retry. In tests there is no sleeping: time is advanced
// through the tester, not by holding a goroutine.
func (a *app) wait(d time.Duration) {
	if a.update == nil {
		return
	}
	time.Sleep(d)
}

func (a *app) fail(what string, err error) {
	a.busy = false
	a.errMsg = errorText(err)
	a.log.Error("%s: %v", what, err)
}

// --- screen actions -------------------------------------------------------

func (a *app) startIdentify() {
	a.screen = screenIdentify
	a.digits = ""
	a.blank = false
	a.errMsg = ""
}

// startBlank is for patients with no record in SIMPUS yet. The server only
// issues a number (mode=blank); registration still happens at the counter.
func (a *app) startBlank() {
	a.blank = true
	a.lookupRes = nil
	a.poli = nil
	a.doctor = nil
	a.errMsg = ""
	a.screen = screenPoli
	a.loadPolis()
}

func (a *app) search() {
	// The identity type and the digits sent are read in one place, from the
	// box content as-is. The Cari button is already dead if the digits are invalid.
	parsed := ParseIdentifier(a.digits)
	if !parsed.OK() {
		return
	}
	identType, value := parsed.Kind, parsed.Value
	a.busy, a.errMsg = true, ""

	gen := a.gen

	a.async(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		res, err := a.api.Lookup(ctx, identType, value, false)
		return func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.fail("lookup", err)
				return
			}
			a.lookupRes = res
			a.log.Event("lookup jenis=%s mode=%s", identType, res.Mode)
			a.screen = screenConfirm
		}
	})
}

func (a *app) loadPolis() {
	a.busy, a.errMsg = true, ""
	gen := a.gen

	a.async(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		list, err := a.api.Poli(ctx)
		return func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.fail("daftar poli", err)
				return
			}
			a.polis = list
		}
	})
}

func (a *app) choosePoli(p Poli) {
	poli := p
	a.poli = &poli
	a.doctor = nil
	a.errMsg = ""
	a.screen = screenDoctor
	a.loadDoctors(p.ID)
}

func (a *app) loadDoctors(poliID string) {
	a.busy = true
	gen := a.gen

	a.async(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		list, err := a.api.Doctors(ctx, poliID)
		return func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.fail("daftar dokter", err)
				return
			}
			a.doctors = list.Dokter
			// One doctor: nothing to choose.
			if list.Otomatis && list.Dipilih != nil {
				a.doctor = list.Dipilih
				a.screen = screenSummary
			}
		}
	})
}

func (a *app) queueBody() QueueBody {
	body := QueueBody{
		PoliID:     a.poli.ID,
		KodeDokter: a.doctor.KodeDokter,
		PoscheckID: a.cfg.PoscheckID,
		DryRun:     a.cfg.DryRun,
	}
	// Mode is decided in one place via ModeForLookup, then the body follows
	// the server rules: blank mode rejects identity and petugas_id
	// (400 INVALID_INPUT).
	mode := "blank"
	if !a.blank && a.lookupRes != nil {
		mode = ModeForLookup(a.lookupRes)
	}
	body.Mode = mode
	if mode != "registration" {
		return body
	}

	body.PetugasID = a.cfg.PetugasID
	// pcare=true so "Nama Faskes" is filled in SIMPUS.
	body.PCare = true
	// The identity type is read from the digit count, same as during lookup.
	parsed := ParseIdentifier(a.digits)
	switch parsed.Kind {
	case kindRM:
		body.NoRM = parsed.Value
	case kindNIK:
		body.NIK = parsed.Value
	case kindBPJS:
		body.NoBPJS = parsed.Value
	}
	return body
}

// showTicket is the single place a successful queue lands on screen. Both the
// normal path and ALREADY_REGISTERED come through here, so the ticket is
// printed exactly once per number.
func (a *app) showTicket(res *QueueResult) {
	a.result = res
	a.screen = screenTicket
	a.printTicket(res)
}

// printTicket sends the receipt to the printer. Best effort: the number is
// already on screen and in SIMPUS, so a dead printer must not stop the kiosk
// or alarm the patient. Failures are logged for staff and nothing else.
func (a *app) printTicket(res *QueueResult) {
	if a.prn == nil || res == nil {
		return
	}
	r := receiptFrom(res, a.now)
	job := encodeReceipt(r, a.cfg.Printer.printerWidth())
	copies := a.cfg.Printer.printerCopies()

	// Printing runs off the UI thread: a slow or unreachable printer must not
	// freeze the kiosk on the ticket screen.
	a.async(func() func() {
		err := a.prn.Print(job, copies)
		return func() {
			if err != nil {
				a.log.Error("cetak tiket kode=%s gagal: %v", r.Code, err)
				return
			}
			a.log.Event("cetak tiket kode=%s copies=%d", r.Code, copies)
		}
	})
}

// submit takes a queue number. QUEUE_BUSY is retried at 1s/2s per the
// contract; other errors are shown immediately.
func (a *app) submit() {
	body := a.queueBody()
	a.busy, a.errMsg = true, ""
	gen := a.gen

	a.async(func() func() {
		var (
			res *QueueResult
			err error
		)
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				a.wait(time.Duration(1<<uint(attempt-1)) * time.Second)
			}
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			res, err = a.api.Queue(ctx, body)
			cancel()
			if err == nil || !IsRetryable(err) {
				break
			}
		}
		return func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				// 409 ALREADY_REGISTERED is not a failure: the number is still
				// valid and is sent in the data.
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.Code == "ALREADY_REGISTERED" {
					var existing QueueResult
					if json.Unmarshal(apiErr.Data, &existing) == nil {
						existing.SudahTerdaftar = true
						a.showTicket(&existing)
						a.log.Event("queue sudah terdaftar kode=%s", existing.Queue.Kode)
						return
					}
				}
				a.fail("queue", err)
				return
			}
			a.showTicket(res)
			a.log.Event("queue kode=%s poli=%s dokter=%s mode=%s",
				res.Queue.Kode, res.Poli.Nama, res.Dokter.NamaDokter, res.Mode)
		}
	})
}

func main() {
	cfg, path, err := LoadConfig()
	if err != nil {
		log.Fatalf("konfigurasi: %v", err)
	}
	lg := NewLogger(filepath.Join(exeDir(), "log"))
	defer lg.Close()
	lg.Event("kiosk mulai config=%s mock=%t dry_run=%t server=%s", path, cfg.Mock, cfg.DryRun, cfg.ServerURL)

	var api apiClient = NewClient(cfg.ServerURL, cfg.APIKey)
	if cfg.Mock {
		api = newMockAPI()
		lg.Event("mode mock: data karangan, tidak menyentuh server")
	}
	// The Run entry is reconciled on every start, so moving the app folder or
	// unpacking a new zip repairs itself without a second step for staff.
	syncAutostart(cfg, winRegistry{}, exePath(), lg)
	a := newApp(cfg, lg, api)
	// Printing is off unless config.json names a printer. With it on, a dead
	// printer is a log line, not a failed registration.
	a.prn = printerFromConfig(cfg)
	if a.prn != nil {
		lg.Event("printer aktif: %s", a.prn)
	} else {
		lg.Event("printer mati: nomor hanya tampil di layar")
	}

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(kioskOptions(cfg, a.view))
		a.update = win.Update
		if !cfg.Windowed {
			win.OnClose(func(e *mygo.CloseEvent) { e.PreventDefault() })
		}
		// Window mode is switched from the Petugas dialog. The MyGo window can
		// only be touched from the UI goroutine, and dialog clicks do run there.
		if !cfg.Windowed {
			a.setFullscreen = func(v bool) { applyWindowMode(win, v) }
		}
		// Exit must always succeed. App.Exit ends the process directly without
		// asking a kiosk window that refuses to close.
		a.quit = func() {
			lg.Event("kiosk ditutup petugas")
			lg.Close()
			mygo.App.Exit(0)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
