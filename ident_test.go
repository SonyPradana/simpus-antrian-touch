package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// openIdentify opens the identity screen in a new tester.
func openIdentify(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	a := testApp()
	tt := ui.NewTester(a.view, 1024, 768)
	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	return a, tt
}

// hintOf returns the explanation line under the digit box.
func hintOf(t *testing.T, tt *ui.Tester) string {
	t.Helper()
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "Terbaca sebagai") || strings.HasPrefix(s, "Angka ") ||
			strings.HasPrefix(s, "Nomor RM 6 angka") {
			return s
		}
	}
	t.Fatalf("hint not found, texts: %q", tt.Texts())
	return ""
}

// The three identity types are recognized from the digit count, without the
// patient choosing.
func TestIdentityAutoDetect(t *testing.T) {
	cases := []struct {
		keys  string
		label string
	}{
		{"123456", "Nomor RM"},
		{"1234567890123", "Nomor BPJS"},
		{"3171234567890001", "NIK"},
	}
	for _, c := range cases {
		a, tt := openIdentify(t)
		for _, k := range c.keys {
			clickAll(t, tt, string(k))
		}
		if got := hintOf(t, tt); !strings.Contains(got, c.label) {
			t.Errorf("%s: hint %q, want %q", c.keys, got, c.label)
		}
		if !tt.HasText("Cari") {
			t.Errorf("%s: Search button is missing", c.keys)
		}
		_ = a
	}
}

// The three number-type buttons are gone: the screen has only one box.
// Matched exactly, because the word "NIK" still appears in the explanation
// line.
func TestIdentityHasNoKindButtons(t *testing.T) {
	_, tt := openIdentify(t)
	texts := tt.Texts()
	for _, name := range []string{"Nomor RM", "NIK", "Nomor BPJS"} {
		for _, got := range texts {
			if got == name {
				t.Errorf("choice button %q is still there, texts: %q", name, texts)
			}
		}
	}
}

// A short RM number is padded with zeros, and the digits sent are already six
// digits.
func TestIdentityPadsShortRM(t *testing.T) {
	a, tt := openIdentify(t)
	clickAll(t, tt, "1", "2", "3")

	if a.digits != "123" {
		t.Errorf("box holds %q, want 123: zeros are added on send, not while typing", a.digits)
	}
	if got := hintOf(t, tt); !strings.Contains(got, "000123") {
		t.Errorf("hint %q does not mention 000123", got)
	}
	if got := ParseIdentifier(a.digits).Value; got != "000123" {
		t.Errorf("sent value %q, want 000123", got)
	}
}

// A length that does not match is marked invalid, and Cari is dead.
func TestIdentityRejectsWrongLength(t *testing.T) {
	for _, keys := range []string{"1234567", "123456789012", "12345678901234"} {
		a, tt := openIdentify(t)
		for _, k := range keys {
			clickAll(t, tt, string(k))
		}
		if got := hintOf(t, tt); !strings.Contains(got, "belum sesuai") {
			t.Errorf("%s: hint %q, want the mismatch marker", keys, got)
		}
		_ = tt.Click("Cari") // button is dead: no effect
		if a.screen != screenIdentify {
			t.Errorf("%s: screen %d, want identify: Search must stay disabled", keys, a.screen)
		}
	}
}

// The box stops at 16 digits; further taps add nothing.
func TestIdentityStopsAtSixteenDigits(t *testing.T) {
	a, tt := openIdentify(t)
	for _, k := range "3171234567890001" {
		clickAll(t, tt, string(k))
	}
	clickAll(t, tt, "9", "9")
	if len(a.digits) != maxIdentLen {
		t.Errorf("box holds %d digits, want %d", len(a.digits), maxIdentLen)
	}
}

// Ctrl+V in the box works for staff who plug in a keyboard, without an extra
// on-screen button. Pasted numbers are still filtered down to digits.
func TestIdentityPasteWithKeyboard(t *testing.T) {
	a, tt := openIdentify(t)
	if err := tt.Click("Nomor RM, NIK, atau Nomor BPJS"); err != nil {
		t.Fatalf("click the box: %v", err)
	}
	tt.SetClipboard("0000 1234-5678 9")
	tt.Key(ui.Cmd, ui.KeyV)

	if a.digits != "0000123456789" {
		t.Errorf("box holds %q, want the BPJS number without spaces", a.digits)
	}
	if got := hintOf(t, tt); !strings.Contains(got, "Nomor BPJS") {
		t.Errorf("hint %q does not mention Nomor BPJS", got)
	}
}

// No paste button: Ctrl+V in the box is enough.
func TestIdentityHasNoPasteButton(t *testing.T) {
	_, tt := openIdentify(t)
	for _, got := range tt.Texts() {
		if got == "Tempel" {
			t.Errorf("Paste button is still there, texts: %q", tt.Texts())
		}
	}
}

// The box content can be copied again: staff sometimes need to move it.
func TestIdentityCopyBack(t *testing.T) {
	_, tt := openIdentify(t)
	clickAll(t, tt, "1", "2", "3", "4", "5", "6")
	if err := tt.Click("Nomor RM, NIK, atau Nomor BPJS"); err != nil {
		t.Fatalf("click the box: %v", err)
	}
	tt.Command("selectAll")
	tt.Command("copy")
	if got := tt.Clipboard(); got != "123456" {
		t.Errorf("clipboard %q, want 123456", got)
	}
}

// Letters typed along the way are stripped before sending, and the delete
// button works on digits only.
func TestIdentityKeepsOnlyDigits(t *testing.T) {
	a, tt := openIdentify(t)
	if err := tt.Click("Nomor RM, NIK, atau Nomor BPJS"); err != nil {
		t.Fatalf("click the box: %v", err)
	}
	tt.Type("12ab34")
	if a.digits != "1234" {
		t.Errorf("box holds %q, want 1234", a.digits)
	}
	clickAll(t, tt, "Hapus")
	if a.digits != "123" {
		t.Errorf("after Delete %q, want 123", a.digits)
	}
}
