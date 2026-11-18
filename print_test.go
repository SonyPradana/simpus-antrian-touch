package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakePrinter records what would have been printed. Errors are configurable so
// the non-blocking behaviour can be tested without a real printer.
type fakePrinter struct {
	name   string
	jobs   [][]byte
	copies []int
	err    error
	calls  int
}

func (f *fakePrinter) String() string {
	if f.name == "" {
		return "printer tes"
	}
	return f.name
}

func (f *fakePrinter) Print(job []byte, copies int) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, append([]byte(nil), job...))
	f.copies = append(f.copies, copies)
	return nil
}

func ticketResult() *QueueResult {
	return &QueueResult{
		Mode:   "registration",
		Poli:   Poli{ID: "01", Nama: "Poli Umum"},
		Dokter: Doctor{KodeDokter: "00001", NamaDokter: "dr. Ani"},
		Queue:  Queue{Kode: "A007", Tanggal: "2026-11-18", MenungguAhead: 3},
		Lokasi: "Puskesmas Lerep",
		Patient: &Patient{
			NoRM: "000123",
			Nama: "Siti Rahayu",
		},
	}
}

// The ticket must carry the number, the patient, and the destination. Those
// three are what the patient needs and what staff read back at the counter.
func TestEncodeReceiptContainsTicketFacts(t *testing.T) {
	res := ticketResult()
	now := time.Date(2026, 11, 18, 9, 30, 0, 0, time.Local)
	out := string(encodeReceipt(receiptFrom(res, now), 32))

	for _, want := range []string{"NOMOR ANTRIAN", "A007", "Siti Rahayu", "Poli Umum", "dr. Ani", "2026-11-18", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("ticket is missing %q", want)
		}
	}
}

// ESC/POS needs the initialise and cut sequences, or the printer ignores the
// job or never feeds the paper out.
func TestEncodeReceiptStartsWithInitAndEndsWithCut(t *testing.T) {
	out := encodeReceipt(receiptFrom(ticketResult(), time.Now()), 32)

	if !bytes.HasPrefix(out, escInit) {
		t.Error("ticket must start with ESC @")
	}
	if !bytes.HasSuffix(out, escFeedCut) {
		t.Error("ticket must end with feed and cut")
	}
}

// dry_run must be obvious on paper too: a number that is not in SIMPUS must
// never look like a normal ticket.
func TestEncodeReceiptMarksDryRun(t *testing.T) {
	res := ticketResult()
	res.DryRun = true
	out := string(encodeReceipt(receiptFrom(res, time.Now()), 32))

	if !strings.Contains(out, "MODE UJI") {
		t.Error("dry-run ticket must say MODE UJI")
	}
}

// A new patient has no name yet; the ticket must not show an empty patient
// line or invent one.
func TestEncodeReceiptOmitsEmptyPatient(t *testing.T) {
	res := ticketResult()
	res.Mode = "blank"
	res.Patient = nil
	out := string(encodeReceipt(receiptFrom(res, time.Now()), 32))

	if strings.Contains(out, "Pasien") {
		t.Error("blank mode must not print a patient line")
	}
	if !strings.Contains(out, "Pendaftaran data di loket") {
		t.Error("blank mode must tell the patient to register at the counter")
	}
}

// No line may exceed the paper width, or the printer wraps it and the ticket
// looks broken.
func TestEncodeReceiptFitsWidth(t *testing.T) {
	res := ticketResult()
	res.Poli.Nama = strings.Repeat("Poli Sangat Panjang ", 5)
	res.Patient.Nama = strings.Repeat("Nama Panjang ", 5)

	for _, width := range []int{32, 48} {
		out := string(encodeReceipt(receiptFrom(res, time.Now()), width))
		for _, line := range strings.Split(stripEscPos(out), "\n") {
			if len(line) > width {
				t.Errorf("width %d: line %q is %d chars", width, line, len(line))
			}
		}
	}
}

// stripEscPos removes ESC/POS command sequences so a test can measure what
// actually reaches the paper. Each command this encoder uses is ESC/GS
// followed by one byte and, for the ones used here, one argument byte.
func stripEscPos(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != 0x1B && c != 0x1D {
			b.WriteByte(c)
			continue
		}
		cmd := s[i+1]
		switch cmd {
		case '@': // initialise: no argument
			i++
		case 'a', 'E', '!': // align, bold, size: one argument
			i += 2
		case 'V': // cut: mode + feed
			i += 3
		default:
			i++
		}
	}
	return b.String()
}

// Printing is best effort. A failed print must not clear the result, must not
// leave the kiosk busy, and must be logged rather than surfaced to the patient.
func TestPrintTicketFailureDoesNotBreakTheFlow(t *testing.T) {
	a := testApp()
	a.prn = &fakePrinter{err: errors.New("kertas habis")}
	a.now = time.Now()

	res := ticketResult()
	a.showTicket(res)

	if a.result != res {
		t.Error("a failed print must keep the result on screen")
	}
	if a.screen != screenTicket {
		t.Errorf("screen %d, want the ticket screen", a.screen)
	}
	if a.errMsg != "" {
		t.Errorf("patient must not see a printer error, got %q", a.errMsg)
	}
}

// With a working printer the ticket is printed exactly once per number, and
// the queue screen still appears.
func TestPrintTicketHappensOnce(t *testing.T) {
	a := testApp()
	fake := &fakePrinter{}
	a.prn = fake
	a.now = time.Now()

	a.showTicket(ticketResult())

	if fake.calls != 1 {
		t.Fatalf("print called %d times, want 1", fake.calls)
	}
	if len(fake.jobs) != 1 || len(fake.jobs[0]) == 0 {
		t.Fatal("no ticket bytes were sent")
	}
	if !strings.Contains(string(fake.jobs[0]), "A007") {
		t.Error("printed ticket is missing the queue code")
	}
	if a.screen != screenTicket {
		t.Error("ticket screen must still be shown")
	}
}

// Printing off is the default: no printer configured means no attempt at all.
func TestPrintTicketWithoutPrinterIsNoop(t *testing.T) {
	a := testApp()
	if a.prn != nil {
		t.Fatal("test app must start without a printer")
	}

	a.showTicket(ticketResult()) // must not panic
	if a.screen != screenTicket {
		t.Error("ticket screen must be shown even without a printer")
	}
}

// Copies reach the printer as configured, and a nonsense value is clamped
// rather than sending zero tickets.
func TestPrinterConfigNormalises(t *testing.T) {
	cases := []struct {
		cfg        PrinterConfig
		wantWidth  int
		wantCopies int
	}{
		{PrinterConfig{}, 32, 1},
		{PrinterConfig{Width: 48, Copies: 2}, 48, 2},
		{PrinterConfig{Width: 5, Copies: 0}, 32, 1},
		{PrinterConfig{Width: 500, Copies: -3}, 32, 1},
	}
	for _, c := range cases {
		if got := c.cfg.printerWidth(); got != c.wantWidth {
			t.Errorf("width %d -> %d, want %d", c.cfg.Width, got, c.wantWidth)
		}
		if got := c.cfg.printerCopies(); got != c.wantCopies {
			t.Errorf("copies %d -> %d, want %d", c.cfg.Copies, got, c.wantCopies)
		}
	}
}

// printerFromConfig returns nil when no printer is named. There is no separate
// on/off switch, so the two cannot disagree.
func TestPrinterFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"off by default", &Config{}, false},
		{"named", &Config{Printer: PrinterConfig{Name: "EPSON TM-T82"}}, true},
		{"nil config", nil, false},
	}
	for _, c := range cases {
		got := printerFromConfig(c.cfg)
		if (got != nil) != c.want {
			t.Errorf("%s: printer=%v, want %v", c.name, got, c.want)
		}
	}

	// A name padded with spaces is invisible in a text editor but makes
	// OpenPrinter fail, so LoadConfig trims it before it gets here.
	cfg := &Config{Printer: PrinterConfig{Name: "  EPSON TM-T82  "}}
	cfg.normalize()
	got := printerFromConfig(cfg)
	if got == nil {
		t.Fatal("padded name must still produce a printer")
	}
	if got.String() != "EPSON TM-T82" {
		t.Errorf("printer name %q, want it trimmed", got.String())
	}
}

// A code-page based thermal printer prints bytes above 0x7F as garbage, and a
// multi-byte character would break every column after it.
func TestEncodeReceiptStaysASCII(t *testing.T) {
	res := ticketResult()
	res.Patient.Nama = "Siti Rahayu ñü"
	res.Poli.Nama = "Poli Umum – Anak"
	out := string(encodeReceipt(receiptFrom(res, time.Now()), 32))

	for _, r := range out {
		if r > 0x7F {
			t.Fatalf("non-ASCII %q reached the printer", r)
		}
	}
	if !strings.Contains(out, "Siti Rahayu ??") {
		t.Errorf("non-ASCII characters should become ?, got %q", stripEscPos(out))
	}
}

// No fixed line may be cut. A ".." on a ticket means a sentence lost its end
// on 58 mm paper, which is how the dry-run warning would have read.
func TestEncodeReceiptFitsEveryFixedLine(t *testing.T) {
	dry := ticketResult()
	dry.DryRun = true
	blank := ticketResult()
	blank.Mode = "blank"
	blank.Patient = nil

	now := time.Now()
	for _, r := range []receipt{
		receiptFrom(ticketResult(), now),
		receiptFrom(dry, now),
		receiptFrom(blank, now),
		sampleReceipt(now),
	} {
		out := stripEscPos(string(encodeReceipt(r, 32)))
		if strings.Contains(out, "..") {
			t.Errorf("fixed text is truncated at 32 columns:\n%s", out)
		}
	}
}

// The sample ticket must be recognisable as a test, or it will be handed to a
// patient as a real number.
func TestSampleReceiptIsMarked(t *testing.T) {
	out := string(encodeReceipt(sampleReceipt(time.Now()), 32))
	if !strings.Contains(out, "CONTOH") {
		t.Error("sample ticket must say CONTOH")
	}
	if !strings.Contains(out, "NOMOR ANTRIAN") {
		t.Error("sample ticket should look like a real one")
	}
}

// Staff need to see the outcome without opening a log file: printing is silent
// by design, so the staff menu is the only place a broken printer shows up.
func TestTestPrintReportsResult(t *testing.T) {
	a := testApp()
	a.now = time.Now()

	good := &fakePrinter{name: "EPSON TM-T82"}
	a.prn = good
	a.testPrint()
	if !strings.Contains(a.printNote, "EPSON TM-T82") {
		t.Errorf("success note %q should name the printer", a.printNote)
	}
	if good.calls != 1 {
		t.Errorf("print called %d times, want 1", good.calls)
	}

	bad := &fakePrinter{name: "EPSON TM-T82", err: errors.New("kertas habis")}
	a.prn = bad
	a.testPrint()
	if !strings.Contains(a.printNote, "kertas habis") {
		t.Errorf("failure note %q should carry the reason", a.printNote)
	}
}

// The staff menu must state whether printing is on, and offer a test only when
// there is a printer to test.
func TestStaffMenuShowsPrinterState(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		a := testApp()
		tt := newTester(a)
		clickAll(t, tt, "Petugas")
		if !tt.HasText("Printer: mati") {
			t.Errorf("menu should say printing is off, texts: %q", tt.Texts())
		}
		if tt.HasText("Tes cetak") {
			t.Error("no test button without a printer")
		}
	})

	t.Run("configured", func(t *testing.T) {
		a := testApp()
		a.prn = &fakePrinter{name: "EPSON TM-T82"}
		tt := newTester(a)
		clickAll(t, tt, "Petugas")
		if !tt.HasText("EPSON TM-T82") {
			t.Errorf("menu should name the printer, texts: %q", tt.Texts())
		}
		clickAll(t, tt, "Tes cetak")
		if !tt.HasText("Terkirim ke") {
			t.Errorf("menu should report the result, texts: %q", tt.Texts())
		}
	})
}
