package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// body picks the active screen. Each screen is small and has one primary
// action, so elderly patients do not have to choose among many buttons.
func (a *app) body(c *ui.Context) {
	switch a.screen {
	case screenHome:
		a.home(c)
	case screenIdentify:
		a.identify(c)
	case screenConfirm:
		a.confirm(c)
	case screenPoli:
		a.poliScreen(c)
	case screenDoctor:
		a.doctorScreen(c)
	case screenSummary:
		a.summary(c)
	case screenTicket:
		a.ticket(c)
	}
}

// hint shows a warning in the last 20 seconds before the screen returns to
// start. Before that it is empty, so the countdown does not distract.
func (a *app) hint() string {
	if a.screen == screenHome || a.touchedAt.IsZero() || a.staffOpen {
		return ""
	}
	left := idleTimeout - a.now.Sub(a.touchedAt)
	if left > idleTimeout-idleWarn {
		return ""
	}
	if left < 0 {
		left = 0
	}
	// Rounded up: 19.2 seconds left shows "20 seconds", so the number is
	// never smaller than the time actually remaining.
	secs := int((left + time.Second - 1) / time.Second)
	return fmt.Sprintf("Layar kembali ke awal dalam %d detik", secs)
}

func (a *app) home(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(24).Children(func() {
		ui.Text(c, "Selamat datang").FontSize(40).Bold()
		ui.Text(c, "Silakan pilih jenis kunjungan Anda").FontSize(20).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(24).Justify(ui.Center).Children(func() {
			if ui.PrimaryButton(c.Key("identify"), "Punya Nomor RM / NIK / BPJS").
				FontSize(24).Size(420, 160).Clicked() {
				a.touch()
				a.startIdentify()
			}
			if ui.Button(c.Key("blank"), "Pasien Baru").
				FontSize(24).Size(420, 160).Clicked() {
				a.touch()
				a.startBlank()
			}
		})
	})
}

func (a *app) identify(c *ui.Context) {
	// One box for three number types: the patient does not have to pick a
	// type first, the digit count decides. The box is a TextInput, not a static
	// digit field, so a number can be pasted from the clipboard — and copied
	// again if staff need it.
	//
	// Its content is cleaned every frame. Stray letters and pasted numbers
	// with spaces are gone before they reach the server; the box catches up a
	// frame later.
	a.digits = CleanIdentifier(a.digits)
	parsed := ParseIdentifier(a.digits)
	hint, wrong := identHint(a.digits)

	ui.Row(c).Gap(32).Grow(1).Children(func() {
		ui.Column(c).Grow(1).Gap(16).Children(func() {
			ui.Text(c, "Masukkan identitas Anda").FontSize(24).Bold()
			// TextInput, not a static digit field: a number can be pasted with
			// Ctrl+V and copied again. The 16-digit limit is enforced in
			// CleanIdentifier, not MaxLength, so spaces in a pasted number do
			// not eat into the digit budget.
			ui.TextInput(c.Key("ident"), &a.digits).
				FontSize(36).Height(96).
				Placeholder("Nomor RM / NIK / Nomor BPJS").
				Label("Nomor RM, NIK, atau Nomor BPJS")

			color := c.Theme().TextMuted
			if wrong {
				color = c.Theme().Danger
			}
			ui.Text(c, hint).FontSize(18).TextColor(color)

			keypad(c, &a.digits, a.touch)
		})

		ui.Column(c).Width(320).Gap(14).Children(func() {
			ui.Text(c, "Nomor RM ada di kartu berobat. NIK ada di KTP. Nomor BPJS ada di kartu BPJS.").
				FontSize(16).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			if ui.PrimaryButton(c.Key("search"), "Cari").
				FontSize(22).Height(72).Disabled(a.busy || !parsed.OK()).Clicked() {
				a.touch()
				a.search()
			}
			if ui.Button(c.Key("back"), "Kembali").FontSize(18).Height(56).Clicked() {
				a.touch()
				a.reset()
			}
		})
	})
}

func (a *app) confirm(c *ui.Context) {
	res := a.lookupRes
	ui.Column(c).Grow(1).Gap(18).Children(func() {
		if res == nil || res.Patient == nil {
			ui.Text(c, "Data Anda belum ada di puskesmas").FontSize(26).Bold()
			ui.Text(c, "Nomor antrian tetap bisa diambil. Pendaftaran data dilakukan di loket.").
				FontSize(18).TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, "Apakah ini Anda?").FontSize(26).Bold()
			ui.Column(c).Gap(6).Padding(20).Radius(12).
				Background(c.Theme().Surface).Children(func() {
				ui.Text(c, res.Patient.Nama).FontSize(30).Bold()
				ui.Textf(c, "No. RM %s", res.Patient.NoRM).FontSize(18)
				if res.Patient.TanggalLahir != "" {
					ui.Textf(c, "Lahir %s", res.Patient.TanggalLahir).FontSize(18)
				}
				if res.Patient.Alamat != "" {
					ui.Textf(c, "%s, %s", res.Patient.Alamat, res.Patient.Desa).FontSize(18)
				}
				ui.Text(c, bpjsLine(res.BPJS)).FontSize(18).TextColor(bpjsColor(c, res.BPJS))
			})
		}

		ui.Spacer(c)
		ui.Row(c).Gap(16).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("notme"), "Bukan Saya").FontSize(20).Height(64).Clicked() {
				a.touch()
				a.screen = screenIdentify
				a.lookupRes = nil
			}
			label := "Ya, Ini Saya"
			if res == nil || res.Patient == nil {
				label = "Lanjut"
			}
			if ui.PrimaryButton(c.Key("confirm"), label).FontSize(22).Height(64).Clicked() {
				a.touch()
				a.blank = res == nil || res.Patient == nil
				a.screen = screenPoli
				a.loadPolis()
			}
		})
	})
}

func (a *app) poliScreen(c *ui.Context) {
	ui.Text(c, "Pilih Poliklinik").FontSize(26).Bold()
	if len(a.polis) == 0 {
		a.emptyOrBusy(c, "Belum ada poli yang buka hari ini.")
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Grid(c).Columns(3).Gap(16).Children(func() {
			for _, p := range a.polis {
				poli := p
				ui.Button(c.Key("poli-"+poli.ID), poli.Nama).
					FontSize(22).Height(110).Label("Poli " + poli.Nama).
					OnClick(func() {
						a.touch()
						a.choosePoli(poli)
					})
			}
		})
	})
	a.backRow(c, "Kembali", func() { a.reset() })
}

func (a *app) doctorScreen(c *ui.Context) {
	name := ""
	if a.poli != nil {
		name = a.poli.Nama
	}
	ui.Textf(c, "Pilih Dokter — %s", name).FontSize(26).Bold()
	if len(a.doctors) == 0 {
		a.emptyOrBusy(c, "Belum ada dokter yang praktik di poli ini hari ini.")
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			for _, d := range a.doctors {
				doc := d
				// The schedule goes below the button, not inside it: text inside
				// a button makes the patient's click area inconsistent.
				ui.Button(c.Key("dokter-"+doc.KodeDokter), doc.NamaDokter).
					FontSize(22).Height(72).OnClick(func() {
					a.touch()
					a.doctor = &doc
					a.screen = screenSummary
				})
				ui.Textf(c, "Praktik %s · sisa kuota %d", doc.JamPraktek, doc.Kapasitas).
					FontSize(16).TextColor(c.Theme().TextMuted)
			}
		})
	})
	a.backRow(c, "Ganti Poli", func() {
		a.doctor = nil
		a.screen = screenPoli
	})
}

// backRow puts one back button in the bottom left. Without it the patient
// is locked on the choice screen until the timer runs out.
func (a *app) backRow(c *ui.Context, label string, fn func()) {
	ui.Row(c).Children(func() {
		if ui.Button(c.Key("back"), label).FontSize(18).Height(56).Clicked() {
			a.touch()
			fn()
		}
	})
}

func (a *app) summary(c *ui.Context) {
	ui.Text(c, "Periksa kembali").FontSize(26).Bold()
	ui.Column(c).Gap(10).Padding(20).Radius(12).Background(c.Theme().Surface).Children(func() {
		if a.lookupRes != nil && a.lookupRes.Patient != nil {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "Nama").FontSize(18).Width(160).TextColor(c.Theme().TextMuted)
				ui.Text(c, a.lookupRes.Patient.Nama).FontSize(20).Bold()
			})
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "No. RM").FontSize(18).Width(160).TextColor(c.Theme().TextMuted)
				ui.Text(c, a.lookupRes.Patient.NoRM).FontSize(20)
			})
		}
		if a.poli != nil {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "Poli").FontSize(18).Width(160).TextColor(c.Theme().TextMuted)
				ui.Text(c, a.poli.Nama).FontSize(20)
			})
		}
		if a.doctor != nil {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "Dokter").FontSize(18).Width(160).TextColor(c.Theme().TextMuted)
				ui.Text(c, a.doctor.NamaDokter).FontSize(20)
			})
		}
		if a.lookupRes != nil {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "BPJS").FontSize(18).Width(160).TextColor(c.Theme().TextMuted)
				ui.Text(c, bpjsLine(a.lookupRes.BPJS)).FontSize(20).
					TextColor(bpjsColor(c, a.lookupRes.BPJS))
			})
		}
	})

	ui.Spacer(c)
	ui.Row(c).Gap(16).Justify(ui.End).Children(func() {
		if ui.Button(c.Key("back"), "Kembali").FontSize(20).Height(64).Clicked() {
			a.touch()
			if len(a.doctors) > 1 {
				a.screen = screenDoctor
			} else {
				a.screen = screenPoli
			}
		}
		if ui.PrimaryButton(c.Key("submit"), "Ambil Nomor Antrian").
			FontSize(24).Height(72).Disabled(a.busy).Clicked() {
			a.touch()
			a.submit()
		}
	})
}

func (a *app) ticket(c *ui.Context) {
	res := a.result
	if res == nil {
		a.emptyOrBusy(c, "Nomor antrian tidak tersedia.")
		return
	}
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.Text(c, "Nomor Antrian Anda").FontSize(28).Bold()
		ui.Text(c, res.Queue.Kode).FontSize(120).Bold().TextColor(c.Theme().Accent)
		ui.Textf(c, "%s · %s", res.Poli.Nama, res.Dokter.NamaDokter).FontSize(24)
		if res.SudahTerdaftar {
			ui.Text(c, "Anda sudah terdaftar hari ini. Nomor di atas tetap berlaku.").
				FontSize(18).TextColor(c.Theme().Warning)
		}
		if res.Queue.MenungguAhead > 0 {
			ui.Textf(c, "Menunggu di depan: %d orang", res.Queue.MenungguAhead).
				FontSize(20).TextColor(c.Theme().TextMuted)
		}
		if res.DryRun {
			// This number is only a server calculation: no row in the database and
			// it does not appear on the calling screen. Staff must know the
			// difference.
			ui.Text(c, "MODE UJI — nomor ini tidak tersimpan.").
				FontSize(20).Bold().TextColor(c.Theme().Danger)
		}
		if res.Mode == "blank" {
			// The server only issues a number; new patient data is registered
			// by staff at the counter.
			ui.Text(c, "Pendaftaran data dilakukan di loket.").
				FontSize(18).TextColor(c.Theme().TextMuted)
		}
		ui.Spacer(c)
		if ui.PrimaryButton(c.Key("done"), "Selesai").FontSize(22).Height(64).Width(280).Clicked() {
			a.touch()
			a.reset()
		}
	})
}

// emptyOrBusy explains why a list is empty: still loading, or genuinely
// nothing to show.
func (a *app) emptyOrBusy(c *ui.Context, empty string) {
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		if a.busy {
			ui.Spinner(c)
			ui.Text(c, "Memuat…").FontSize(20).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, empty).FontSize(20).TextColor(c.Theme().TextMuted)
		if ui.Button(c.Key("back"), "Kembali").FontSize(18).Clicked() {
			a.touch()
			a.reset()
		}
	})
}

// footer holds the Petugas button. The exit used to be only a five-tap
// gesture on an invisible box in the corner: patients never found it, and staff
// missed it too because the target was small. A small button labeled "Petugas"
// is more honest — the kiosk must always have a visible way out.
func (a *app) footer(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		if a.busy {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Mohon tunggu…").FontSize(16).TextColor(c.Theme().TextMuted)
			})
		}
		ui.Spacer(c)
		if ui.Button(c.Key("staff-open"), "Petugas").FontSize(16).Height(44).
			TextColor(c.Theme().TextMuted).Clicked() {
			a.log.Event("tombol petugas ditekan")
			a.staffOpen = true
		}
	})
}

// bpjsLine translates BPJS status into a short sentence. The patient only
// needs to know one thing: covered by BPJS or not.
func bpjsLine(b *BPJSStatus) string {
	if b == nil || !b.IsBPJS {
		return "Umum / non-BPJS"
	}
	switch VerdictOf(b) {
	case BPJSActive:
		return "BPJS aktif"
	case BPJSInactive:
		return "BPJS tidak aktif"
	}
	return "BPJS belum bisa diperiksa"
}

func bpjsColor(c *ui.Context, b *BPJSStatus) ui.Color {
	switch VerdictOf(b) {
	case BPJSActive:
		return c.Theme().Success
	case BPJSInactive:
		return c.Theme().Warning
	}
	return c.Theme().TextMuted
}
