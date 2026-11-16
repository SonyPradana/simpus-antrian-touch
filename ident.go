package main

import "strings"

// The kiosk does not ask for the identity type: the digit count decides.
// Three types are known to the server: Nomor RM, Nomor BPJS, and NIK.
// Other lengths count as typos, not something to guess at.
const (
	kindRM   = "no_rm"
	kindBPJS = "no_bpjs"
	kindNIK  = "nik"

	rmLen   = 6
	bpjsLen = 13
	nikLen  = 16
	// maxIdentLen closes off typos: no identity is longer than NIK.
	maxIdentLen = nikLen
)

// parsedIdent is the result of reading the identity box in one frame.
type parsedIdent struct {
	// An empty Kind means the digits are not usable yet: empty, too
	// long, or a length that matches none of the three types above.
	Kind  string
	Label string
	// Value is the digits sent to the server. An RM number written
	// with fewer than six digits is padded with leading zeros, because staff
	// commonly say "123" for patient number "000123".
	Value string
	// Digits is what was actually typed, without the extra zeros.
	Digits string
}

func (p parsedIdent) OK() bool { return p.Kind != "" }

// CleanIdentifier keeps digits only. Numbers copied from WhatsApp or from a
// card often carry spaces, dots, and hyphens.
func CleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseIdentifier reads digits into an identity type. This is the only place
// the type is decided; screens, lookup, and the queue body all use the result.
func ParseIdentifier(raw string) parsedIdent {
	d := CleanIdentifier(raw)
	p := parsedIdent{Digits: d}
	switch {
	case d == "" || len(d) > maxIdentLen:
		return p
	case len(d) == nikLen:
		p.Kind, p.Label = kindNIK, "NIK"
	case len(d) == bpjsLen:
		p.Kind, p.Label = kindBPJS, "Nomor BPJS"
	case len(d) <= rmLen:
		p.Kind, p.Label = kindRM, "Nomor RM"
	default:
		return p
	}
	p.Value = d
	if p.Kind == kindRM {
		p.Value = strings.Repeat("0", rmLen-len(d)) + d
	}
	return p
}

// identHint is the one-line explanation under the digit box. wrong=true
// means the digits are not valid yet, and the Cari button is disabled.
func identHint(raw string) (text string, wrong bool) {
	d := CleanIdentifier(raw)
	p := ParseIdentifier(d)
	switch {
	case d == "":
		return "Nomor RM 6 angka, NIK 16 angka, atau Nomor BPJS 13 angka.", false
	case len(d) > maxIdentLen:
		return "Angka terlalu panjang. Maksimal 16 angka.", true
	case !p.OK():
		return "Angka belum sesuai. Nomor RM 6, Nomor BPJS 13, atau NIK 16 angka.", true
	case p.Value != d:
		return "Terbaca sebagai " + p.Label + " " + p.Value, false
	default:
		return "Terbaca sebagai " + p.Label + ".", false
	}
}
