package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A missing config.json is not an error: a new kiosk still has to open. An
// explicit CONFIG_FILE is different: it must exist, or the operator is sent
// hunting in the wrong place.
func TestLoadConfigMissingFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("CONFIG_FILE", path)

	// CONFIG_FILE that points nowhere is reported as a path problem, not as
	// an empty api_key.
	_, _, err := LoadConfig()
	if err == nil {
		t.Fatal("a missing CONFIG_FILE must be reported")
	}
	if !strings.Contains(err.Error(), "CONFIG_FILE") || !strings.Contains(err.Error(), path) {
		t.Errorf("error should name CONFIG_FILE and its path, got %q", err)
	}

	// Without CONFIG_FILE the file is optional: an existing kiosk that
	// already has mock=true keeps opening.
	t.Setenv("CONFIG_FILE", "")
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"mock": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfig()
	if err != nil {
		t.Fatalf("config.json in the working folder must be read: %v", err)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("server_url %q, want %q", cfg.ServerURL, defaultServerURL)
	}

	// No file and mock=false refuses to start, and says where it looked.
	if err := os.Remove(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	_, _, err = LoadConfig()
	if err == nil {
		t.Fatal("a missing config.json with mock=false must be reported")
	}
	if !strings.Contains(err.Error(), "tidak ditemukan") {
		t.Errorf("error should name the missing file, got %q", err)
	}

	// Blank api_key with mock=false is still its own message.
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"mock": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = LoadConfig()
	if err == nil {
		t.Fatal("config without api_key must be rejected when mock=false")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error should name api_key, got %q", err)
	}

	if err := os.WriteFile(path, []byte(`{"mock": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	cfg, _, err = LoadConfig()
	if err != nil {
		t.Fatalf("mock=true must pass: %v", err)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("server_url %q, want %q", cfg.ServerURL, defaultServerURL)
	}
	if !cfg.Mock {
		t.Error("mock not read")
	}
}

// A local config.json next to the exe must keep working exactly as before:
// CONFIG_FILE is an addition, not a replacement.
func TestLoadConfigFallsBackToWorkingFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"mock": true, "petugas_id": "7"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", "")
	t.Chdir(dir)

	cfg, path, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Mock || cfg.PetugasID != "7" {
		t.Errorf("local config.json not read: %+v", cfg)
	}
	if filepath.Base(path) != "config.json" {
		t.Errorf("path %q, want the working-folder config.json", path)
	}
}

// The trailing slash in server_url is dropped so the path does not become
// "//api/...".
func TestLoadConfigNormalizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"server_url":"http://localhost:8080/","api_key":" k ","poscheck_id":" 01 "}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)

	cfg, _, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "http://localhost:8080" {
		t.Errorf("server_url %q", cfg.ServerURL)
	}
	if cfg.APIKey != "k" || cfg.PoscheckID != "01" {
		t.Errorf("api_key %q poscheck_id %q", cfg.APIKey, cfg.PoscheckID)
	}
}

func TestLoadConfigRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	if _, _, err := LoadConfig(); err == nil {
		t.Fatal("broken JSON must be an error, not a silent default")
	}
}

// Notepad and PowerShell write a UTF-8 BOM. Staff will edit config.json with
// those editors, so a BOM must not make the kiosk refuse to start.
func TestLoadConfigToleratesBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"mock":true,"dry_run":true}`)...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)

	cfg, _, err := LoadConfig()
	if err != nil {
		t.Fatalf("config with a BOM must be read: %v", err)
	}
	if !cfg.Mock || !cfg.DryRun {
		t.Errorf("config contents not read: %+v", cfg)
	}
}

// The mode mapping follows the CONTRACT.md §4 table; if that changes, this
// test must fail too.
func TestModeForLookup(t *testing.T) {
	cases := []struct {
		mode        string
		canRegister bool
		want        string
	}{
		{"existing_active", true, "registration"},
		{"existing_inactive", false, "blank"},
		{"existing_bpjs_unknown", true, "registration"},
		{"existing_bpjs_unknown", false, "blank"},
		{"new", false, "blank"},
		{"", false, "blank"},
	}
	for _, c := range cases {
		got := ModeForLookup(&LookupResult{Mode: c.mode, CanRegister: c.canRegister})
		if got != c.want {
			t.Errorf("%s (can_register=%t) -> %s, want %s", c.mode, c.canRegister, got, c.want)
		}
	}
}

func TestCleanIdentifier(t *testing.T) {
	if got := CleanIdentifier(" 0000 1234-5678 "); got != "000012345678" {
		t.Errorf("got %q", got)
	}
}

// The digit count decides the type. An RM number written with fewer than six
// digits is padded with leading zeros; other lengths that are not 6/13/16
// count as typos, not something to guess.
func TestParseIdentifier(t *testing.T) {
	cases := []struct {
		in    string
		kind  string
		value string
		ok    bool
	}{
		{"", "", "", false},
		{"1", kindRM, "000001", true},
		{"123", kindRM, "000123", true},
		{"123456", kindRM, "123456", true},
		{"0000123456789", kindBPJS, "0000123456789", true},
		{"3171234567890001", kindNIK, "3171234567890001", true},
		{"1234567", "", "", false},
		{"123456789012", "", "", false},
		{"12345678901234", "", "", false},
		{"123456789012345", "", "", false},
		{"12345678901234567", "", "", false},
	}
	for _, c := range cases {
		got := ParseIdentifier(c.in)
		if got.OK() != c.ok {
			t.Errorf("%q: ok=%t, want %t", c.in, got.OK(), c.ok)
		}
		if got.Kind != c.kind {
			t.Errorf("%q: kind %q, want %q", c.in, got.Kind, c.kind)
		}
		if got.Value != c.value {
			t.Errorf("%q: value %q, want %q", c.in, got.Value, c.value)
		}
	}
}

// A pasted number carries spaces and hyphens; only the digits are used.
func TestParseIdentifierToleratesPaste(t *testing.T) {
	got := ParseIdentifier("3171 2345 6789 0001")
	if got.Kind != kindNIK || got.Value != "3171234567890001" {
		t.Errorf("got %+v", got)
	}
}

// The message under the box must tell the patient what is wrong, not just
// reject the input.
func TestIdentHint(t *testing.T) {
	if text, wrong := identHint(""); wrong || text == "" {
		t.Errorf("empty box: %q wrong=%t", text, wrong)
	}
	if _, wrong := identHint("123456"); wrong {
		t.Error("6 digits must be valid as an RM number")
	}
	if _, wrong := identHint("1234567890123"); wrong {
		t.Error("13 digits must be valid as a BPJS number")
	}
	if _, wrong := identHint("3171234567890001"); wrong {
		t.Error("16 digits must be valid as a NIK")
	}
	for _, bad := range []string{"1234567", "123456789012", "12345678901234567"} {
		if _, wrong := identHint(bad); !wrong {
			t.Errorf("%q should be flagged as mismatched", bad)
		}
	}
	// Short RM number: the message names the digits actually sent.
	if text, _ := identHint("123"); !strings.Contains(text, "000123") {
		t.Errorf("hint %q does not mention the added zeros", text)
	}
}

// The queue body follows the server rules: blank without identity and without
// petugas_id, registration carries both.
func TestQueueBody(t *testing.T) {
	a := testApp()
	a.cfg.PetugasID = "32"
	a.digits = "123456"
	poli := Poli{ID: "01"}
	doctor := Doctor{KodeDokter: "00001"}
	a.poli, a.doctor = &poli, &doctor

	a.blank = true
	blank := a.queueBody()
	if blank.Mode != "blank" {
		t.Errorf("mode %q, want blank", blank.Mode)
	}
	if blank.NoRM != "" || blank.NIK != "" || blank.NoBPJS != "" || blank.PetugasID != "" {
		t.Errorf("blank mode carries identity/staff: %+v", blank)
	}
	if blank.PoscheckID != "01" || blank.PoliID != "01" || blank.KodeDokter != "00001" {
		t.Errorf("blank lost poli/doctor/poscheck: %+v", blank)
	}

	a.blank = false
	a.lookupRes = &LookupResult{Mode: "existing_active", CanRegister: true}
	reg := a.queueBody()
	if reg.Mode != "registration" {
		t.Errorf("mode %q, want registration", reg.Mode)
	}
	if reg.NoRM != "123456" || reg.PetugasID != "32" || !reg.PCare {
		t.Errorf("registration is missing data: %+v", reg)
	}
}

// The shipped example must never write to the database by default. An
// operator who copies it, fills in api_key and poscheck_id, and starts the
// kiosk should see the real flow without creating a visit.
func TestConfigExampleShipsSafeDefaults(t *testing.T) {
	data, err := os.ReadFile("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config.example.json is broken: %v", err)
	}
	if !cfg.DryRun {
		t.Error("config.example.json must ship dry_run=true so a fresh copy never writes to the DB")
	}
	if !cfg.Mock {
		t.Error("config.example.json must ship mock=true so it runs without a server")
	}
	if cfg.APIKey != "" || cfg.PoscheckID != "" {
		t.Error("config.example.json must not contain a real api_key/poscheck_id")
	}
}

// dry_run is passed through as-is: a kiosk tested against the real server
// must not write fake queue entries if config.json asks for it.
func TestQueueBodyDryRun(t *testing.T) {
	for _, want := range []bool{false, true} {
		a := testApp()
		a.cfg.DryRun = want
		poli := Poli{ID: "01"}
		doctor := Doctor{KodeDokter: "00001"}
		a.poli, a.doctor = &poli, &doctor
		a.blank = true

		if got := a.queueBody().DryRun; got != want {
			t.Errorf("dry_run %t, want %t", got, want)
		}
	}
}

// NIK and BPJS numbers must go into the right field, and a short RM number is
// sent already padded with zeros.
func TestQueueBodyIdentifierFields(t *testing.T) {
	cases := []struct {
		digits string
		field  string
		want   string
	}{
		{"123", "no_rm", "000123"},
		{"123456", "no_rm", "123456"},
		{"0000123456789", "no_bpjs", "0000123456789"},
		{"3171234567890001", "nik", "3171234567890001"},
	}
	for _, c := range cases {
		a := testApp()
		a.digits = c.digits
		poli := Poli{ID: "01"}
		doctor := Doctor{KodeDokter: "00001"}
		a.poli, a.doctor = &poli, &doctor
		a.lookupRes = &LookupResult{Mode: "existing_active", CanRegister: true}

		body := a.queueBody()
		var got string
		switch c.field {
		case "no_rm":
			got = body.NoRM
		case "nik":
			got = body.NIK
		case "no_bpjs":
			got = body.NoBPJS
		}
		if got != c.want {
			t.Errorf("%q (%s): got %q, want %q", c.digits, c.field, got, c.want)
		}
		// Only one identity field may be filled: the server rejects
		// combinations.
		filled := 0
		for _, v := range []string{body.NoRM, body.NIK, body.NoBPJS} {
			if v != "" {
				filled++
			}
		}
		if filled != 1 {
			t.Errorf("%q: %d identity fields filled, want 1", c.digits, filled)
		}
	}
}

// An unverified BPJS status must not appear as active.
func TestVerdictOf(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		in   *BPJSStatus
		want BPJSVerdict
	}{
		{"nil", nil, BPJSUnknown},
		{"belum terverifikasi", &BPJSStatus{Aktif: &yes}, BPJSUnknown},
		{"aktif", &BPJSStatus{Terverifikasi: true, Aktif: &yes}, BPJSActive},
		{"inactive", &BPJSStatus{Terverifikasi: true, Aktif: &no}, BPJSInactive},
		{"no status", &BPJSStatus{Terverifikasi: true}, BPJSUnknown},
	}
	for _, c := range cases {
		if got := VerdictOf(c.in); got != c.want {
			t.Errorf("%s -> %d, want %d", c.name, got, c.want)
		}
	}
}

// Every error code from the server's mapError must have its own sentence, so
// patients do not read a generic message for a problem that can be explained.
// The code list comes from server/internal/httpapi/httpapi.go mapError.
func TestErrorTextCoversServerCodes(t *testing.T) {
	generic := errorText(errors.New("jaringan putus"))

	codes := []string{
		"UNAUTHORIZED", "APM_NOT_CONFIGURED", "QUOTA_EXCEEDED", "POLI_NOT_FOUND",
		"DOCTOR_NOT_FOUND", "DOCTOR_REQUIRED", "BPJS_INACTIVE", "BPJS_INCOMPLETE",
		"BPJS_DOB_MISMATCH", "BPJS_POLI_MISSING", "PCARE_UNAVAILABLE", "QUEUE_BUSY",
		"DB_UNAVAILABLE", "IDENTIFIER_MISMATCH", "PATIENT_NOT_FOUND",
	}
	for _, code := range codes {
		got := errorText(&APIError{Code: code, Status: 400})
		if got == "" || got == generic {
			t.Errorf("%s fell back to the generic message %q", code, got)
		}
	}

	// Misconfiguration on the staff side: the patient is only sent to staff,
	// not asked to fix anything.
	const toStaff = "Terjadi kesalahan. Silakan panggil petugas."
	for _, code := range []string{"PETUGAS_NOT_FOUND", "POSCHECK_NOT_FOUND", "INVALID_INPUT"} {
		if got := errorText(&APIError{Code: code}); got != toStaff {
			t.Errorf("%s -> %q, want %q", code, got, toStaff)
		}
	}

	// Non-API errors (network down) must also become a sentence, not a panic.
	if generic == "" {
		t.Error("network error has no message")
	}
}

// Only momentary disruptions are worth retrying; business errors will not
// change outcome when retried.
func TestIsRetryable(t *testing.T) {
	cases := map[string]bool{
		"QUEUE_BUSY":        true,
		"DB_UNAVAILABLE":    true,
		"QUOTA_EXCEEDED":    false,
		"PATIENT_NOT_FOUND": false,
		"BPJS_INACTIVE":     false,
	}
	for code, want := range cases {
		if got := IsRetryable(&APIError{Code: code}); got != want {
			t.Errorf("%s -> %t, want %t", code, got, want)
		}
	}
	if !IsRetryable(errors.New("dial tcp: timeout")) {
		t.Error("network error must count as retryable")
	}
}
