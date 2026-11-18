package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Receipt printing is best effort by design. A kiosk at the registration desk
// must keep issuing numbers when the paper runs out, the printer is unplugged,
// or the spooler is down: the number is already on screen and in SIMPUS. Every
// failure is logged, never shown to the patient and never retried in a loop
// that could block the queue.

// printer is the one thing the kiosk needs from a printer. Narrow on purpose:
// tests use a fake, and the USB/RAW details stay in one place.
type printer interface {
	// Print writes one complete receipt. Returning an error means "no paper
	// came out", which the caller logs and ignores.
	Print(job []byte, copies int) error
	// String is the printer name, shown in the staff menu so staff can see
	// which printer is configured without opening config.json.
	String() string
}

// receipt is everything printed on one ticket. Built from QueueResult so the
// paper and the screen can never disagree about the number.
type receipt struct {
	Code       string
	Patient    string
	Poli       string
	Doctor     string
	Location   string
	Date       string
	Waiting    int
	DryRun     bool
	Registered bool // already registered today: same number, still valid
	Blank      bool // new patient: data still registered at the counter
	Sample     bool // printer test from the staff menu, never a real number
	PrintedAt  time.Time
}

// receiptFrom maps a queue result onto a receipt. Missing pieces (a new
// patient has no name yet) are left empty rather than invented.
func receiptFrom(res *QueueResult, now time.Time) receipt {
	r := receipt{
		Code:       res.Queue.Kode,
		Poli:       res.Poli.Nama,
		Doctor:     res.Dokter.NamaDokter,
		Location:   res.Lokasi,
		Date:       res.Queue.Tanggal,
		Waiting:    res.Queue.MenungguAhead,
		DryRun:     res.DryRun,
		Registered: res.SudahTerdaftar,
		Blank:      res.Mode == "blank",
		PrintedAt:  now,
	}
	if res.Patient != nil {
		r.Patient = res.Patient.Nama
	}
	return r
}

// sampleReceipt is the ticket printed by the staff "Tes cetak" button. It
// looks like a real one on purpose, so staff judge paper width and cut
// quality, but it says SAMPLE so it can never be mistaken for a number.
func sampleReceipt(now time.Time) receipt {
	return receipt{
		Code:      "A-000",
		Patient:   "Contoh Pasien",
		Poli:      "Poli Umum",
		Doctor:    "dr. Contoh",
		Location:  "Tes cetak",
		Date:      now.Format("2006-01-02"),
		Waiting:   3,
		Sample:    true,
		PrintedAt: now,
	}
}

// ESC/POS control sequences. Only what a plain text ticket needs; nothing
// vendor specific, so cheap 58 mm and 80 mm printers both work.
var (
	escInit     = []byte{0x1B, 0x40}             // reset
	escAlignCtr = []byte{0x1B, 0x61, 0x01}       // centre
	escAlignLft = []byte{0x1B, 0x61, 0x00}       // left
	escBoldOn   = []byte{0x1B, 0x45, 0x01}       // emphasised on
	escBoldOff  = []byte{0x1B, 0x45, 0x00}       // emphasised off
	escDoubleHW = []byte{0x1B, 0x21, 0x30}       // double height + width
	escNormal   = []byte{0x1B, 0x21, 0x00}       // normal size
	escFeedCut  = []byte{0x1D, 0x56, 0x42, 0x00} // feed then partial cut
)

// encodeReceipt turns a receipt into ESC/POS bytes. Pure so tests can assert
// the exact bytes without a printer on hand.
//
// width is the number of characters per line: 32 for 58 mm paper, 48 for
// 80 mm.
func encodeReceipt(r receipt, width int) []byte {
	if width < 20 {
		width = 32
	}
	var b bytes.Buffer

	write := func(seq []byte) { b.Write(seq) }
	line := func(format string, args ...any) {
		b.WriteString(fmt.Sprintf(format, args...))
		b.WriteByte('\n')
	}
	centre := func(text string) {
		line("%s", centrePad(text, width))
	}
	rule := func() {
		line("%s", strings.Repeat("-", width))
	}
	// Double-width text halves the usable column count.
	centreBig := func(text string) {
		write(escDoubleHW)
		centre(fitWidth(text, width/2))
		write(escNormal)
	}

	write(escInit)

	// Header: what this piece of paper is.
	write(escAlignCtr)
	write(escBoldOn)
	centre("NOMOR ANTRIAN")
	write(escBoldOff)

	// The number is the reason the ticket exists, so it is the largest text
	// on the paper.
	write(escBoldOn)
	centreBig(r.Code)
	write(escBoldOff)

	if r.Sample {
		// Printed from the staff menu. Big and first, so a test ticket left
		// on the desk is never handed to a patient as a real number.
		write(escBoldOn)
		centre("CONTOH - TES CETAK")
		write(escBoldOff)
	}
	if r.DryRun {
		// Same warning as the screen: this number is not in SIMPUS. Two
		// lines because the full sentence does not fit 58 mm paper, and a
		// truncated warning is worse than no warning.
		write(escBoldOn)
		centre("*** MODE UJI ***")
		write(escBoldOff)
		centre("TIDAK TERSIMPAN DI SIMPUS")
	}
	if r.Registered {
		centre("(sudah terdaftar hari ini)")
	}

	write(escAlignLft)
	rule()

	// Body: only the lines that carry information. field keeps every value
	// inside the paper width, whatever the label length.
	field := func(label, value string) {
		if value == "" {
			return
		}
		prefix := label + ": "
		line("%s", prefix+fitWidth(value, width-len(prefix)))
	}
	field("Pasien", r.Patient)
	field("Poli", r.Poli)
	field("Dokter", r.Doctor)
	field("Tanggal", r.Date)
	field("Lokasi", r.Location)
	if r.Waiting > 0 {
		line("Menunggu di depan: %d", r.Waiting)
	}

	rule()

	write(escAlignCtr)
	if r.Blank {
		// The number is issued, but patient data is still entered at the
		// counter. Saying so on the ticket saves a question at the desk.
		centre("Pendaftaran data di loket")
	}
	if r.Sample {
		centre("INI TES, BUKAN NOMOR ANTRIAN")
	} else {
		centre("Simpan tiket ini")
	}
	line("")
	line("%s", r.PrintedAt.Format("02-01-2006 15:04"))

	write(escFeedCut)

	return b.Bytes()
}

// centrePad pads text so it sits in the middle of a width column. Text wider
// than the column is left as-is; the printer wraps it.
func centrePad(text string, width int) string {
	text = fitWidth(text, width)
	if pad := width - len(text); pad > 0 {
		return strings.Repeat(" ", pad/2) + text
	}
	return text
}

// fitWidth drops characters beyond width and marks the cut with "..". A
// receipt printer has no word wrap, so long polyclinic names would otherwise
// push the value off the paper.
//
// Text is also reduced to ASCII. Thermal printers boot into a single-byte code
// page, not UTF-8: bytes above 0x7F come out as garbage, and a two-byte
// character would throw off every column after it.
func fitWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = asciiOnly(strings.TrimSpace(text))
	if len(text) <= width {
		return text
	}
	if width <= 2 {
		return text[:width]
	}
	return text[:width-2] + ".."
}

// asciiOnly replaces anything outside ASCII with "?", keeping one column per
// original character. Clean ASCII returns without allocating.
func asciiOnly(text string) string {
	for i := 0; i < len(text); i++ {
		if text[i] > 0x7F {
			return strings.Map(func(r rune) rune {
				if r > 0x7F {
					return '?'
				}
				return r
			}, text)
		}
	}
	return text
}
