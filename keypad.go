package main

import "github.com/egoist/mygo/ui"

// keypad builds the on-screen number keyboard. Big digits, wide touch
// targets, no unnecessary buttons — elderly patients use one finger.
//
// The delete button is written as a word, not an arrow glyph: the kiosk's
// system font does not always have a "⌫" glyph and renders an empty box.
func keypad(c *ui.Context, digits *string, onTouch func()) {
	keys := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	ui.Grid(c).Columns(3).Gap(14).Children(func() {
		for _, k := range keys {
			key := k
			if ui.Button(c.Key("digit-"+key), key).FontSize(38).Height(88).Clicked() {
				if appendDigit(digits, key) {
					onTouch()
				}
			}
		}
		if ui.Button(c.Key("keypad-clear"), "Kosongkan").Label("Kosongkan semua").
			FontSize(20).Height(88).Clicked() {
			*digits = ""
			onTouch()
		}
		if ui.Button(c.Key("digit-0"), "0").FontSize(38).Height(88).Clicked() {
			if appendDigit(digits, "0") {
				onTouch()
			}
		}
		if ui.Button(c.Key("keypad-back"), "Hapus").Label("Hapus satu angka").
			FontSize(20).Height(88).Clicked() {
			d := CleanIdentifier(*digits)
			if d != "" {
				*digits = d[:len(d)-1]
			} else {
				*digits = ""
			}
			onTouch()
		}
	})
}

// appendDigit appends one digit at the end and reports whether the box
// changed. The box stops at 16 digits: NIK is the longest number, so further
// taps add nothing.
func appendDigit(digits *string, d string) bool {
	clean := CleanIdentifier(*digits)
	if len(clean) >= maxIdentLen {
		return false
	}
	*digits = clean + d
	return true
}
