package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestLiveContract checks the real server's data shapes against the types in
// api.go. Only runs when APM_LIVE=1, because it needs a live server and
// config.json with a real api_key.
//
// Every call here is read-only or dry_run: no row is written to the database.
func TestLiveContract(t *testing.T) {
	if os.Getenv("APM_LIVE") != "1" {
		t.Skip("set APM_LIVE=1 to test against the real server")
	}

	cfg, path, err := LoadConfig()
	if err != nil {
		t.Fatalf("config %s: %v", path, err)
	}
	if cfg.Mock {
		t.Fatal("config.json still has mock=true; this test needs the real server")
	}
	api := NewClient(cfg.ServerURL, cfg.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Options: the poscheck_id in config must be known to the server.
	opts, err := api.Options(ctx)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(opts.Poscheck) == 0 {
		t.Error("options.poscheck is empty")
	}
	found := false
	for _, p := range opts.Poscheck {
		if p.ID == cfg.PoscheckID {
			found = true
		}
	}
	if !found {
		t.Errorf("poscheck_id %q is not among options (%d choices)", cfg.PoscheckID, len(opts.Poscheck))
	}
	t.Logf("options: %d petugas, %d poscheck, terpilih lokasi=%q",
		len(opts.Petugas), len(opts.Poscheck), opts.Terpilih.Lokasi)

	// 2. Poli.
	polis, err := api.Poli(ctx)
	if err != nil {
		t.Fatalf("poli: %v", err)
	}
	if len(polis) == 0 {
		t.Fatal("poli list is empty: cannot continue")
	}
	first := polis[0]
	t.Logf("poli: %d, pertama id=%q nama=%q prefix=%q", len(polis), first.ID, first.Nama, first.Prefix)
	if first.ID == "" || first.Nama == "" {
		t.Errorf("poli without id/name: %+v", first)
	}

	// 3. Doctors for the first poli. May be empty (no schedule today).
	docs, err := api.Doctors(ctx, first.ID)
	if err != nil {
		t.Fatalf("dokter poli %q: %v", first.ID, err)
	}
	t.Logf("dokter: %d, otomatis=%t", len(docs.Dokter), docs.Otomatis)
	if docs.Poli.ID == "" {
		t.Error("doctor response without poli")
	}

	// 4. Look up a patient that certainly does not exist: mode must be `new`.
	//    This number is made up, it is not anyone's identity.
	look, err := api.Lookup(ctx, "no_rm", "999999999", false)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if look.Mode != "new" {
		t.Errorf("lookup of an invented number -> mode %q, want new", look.Mode)
	}
	if look.Patient != nil {
		t.Error("mode new but a patient is present")
	}
	t.Logf("invented lookup: mode=%q fallback=%q reason=%q", look.Mode, look.Fallback, look.Reason)

	// 5. Queue dry-run in blank mode: read-only, writes nothing.
	//    Doctor quota may be exhausted, so try poli one by one.
	var (
		ok      *QueueResult
		lastErr error
		tried   int
	)
	for _, p := range polis {
		docs, err := api.Doctors(ctx, p.ID)
		if err != nil {
			t.Logf("poli %q: dokter gagal: %v", p.Nama, err)
			continue
		}
		if len(docs.Dokter) == 0 {
			continue
		}
		tried++
		res, err := api.Queue(ctx, QueueBody{
			Mode:       "blank",
			PoliID:     p.ID,
			KodeDokter: docs.Dokter[0].KodeDokter,
			PoscheckID: cfg.PoscheckID,
			DryRun:     true,
		})
		if err != nil {
			lastErr = err
			t.Logf("poli %q dokter %q: %v", p.Nama, docs.Dokter[0].NamaDokter, err)
			continue
		}
		ok = res
		t.Logf("queue dry-run OK: poli=%q dokter=%q kode=%q lokasi=%q sisa_kuota=%d",
			res.Poli.Nama, res.Dokter.NamaDokter, res.Queue.Kode, res.Lokasi, res.Queue.SisaKuota)
		break
	}
	if tried == 0 {
		t.Skip("no polyclinic has a doctor today")
	}
	if ok == nil {
		t.Fatalf("all %d poli failed the dry-run queue, last: %v", tried, lastErr)
	}
	if !ok.DryRun {
		t.Error("response did not mark dry_run")
	}
	if ok.Queue.Kode == "" {
		t.Error("dry-run did not return a queue code")
	}
	if ok.Mode != "blank" {
		t.Errorf("mode %q, want blank", ok.Mode)
	}
}

// TestLiveUI runs the kiosk screens against the real server, stopping at the
// summary. The "Ambil Nomor Antrian" button is NOT pressed: that would write a
// real row to the database.
func TestLiveUI(t *testing.T) {
	if os.Getenv("APM_LIVE") != "1" {
		t.Skip("set APM_LIVE=1 to test against the real server")
	}
	cfg, path, err := LoadConfig()
	if err != nil {
		t.Fatalf("config %s: %v", path, err)
	}
	if cfg.Mock {
		t.Fatal("config.json still has mock=true")
	}

	a := newApp(cfg, nil, NewClient(cfg.ServerURL, cfg.APIKey))
	tt := newTester(a)

	// Made-up RM number: the server answers mode=new, screen 3 offers
	// "Lanjut" without patient identity. The digits are varied so clicks do not
	// collide with digit text on screen.
	clickAll(t, tt, "Punya Nomor RM / NIK / BPJS")
	clickAll(t, tt, "9", "8", "7", "6", "5", "4", "Cari")
	if !tt.HasText("Data Anda belum ada di puskesmas") {
		t.Fatalf("new-patient confirmation screen did not appear, texts: %q", tt.Texts())
	}
	clickAll(t, tt, "Lanjut")

	if a.screen != screenPoli || len(a.polis) == 0 {
		t.Fatalf("screen %d with %d poli, want a filled poli list", a.screen, len(a.polis))
	}
	// Real poli names must reach the screen.
	if !tt.HasText(a.polis[0].Nama) {
		t.Errorf("poli %q is not on screen, texts: %q", a.polis[0].Nama, tt.Texts())
	}
	t.Logf("poli screen shows %d polyclinics, first %q", len(a.polis), a.polis[0].Nama)

	// Pick a poli that has doctors: with a single doctor, the kiosk goes straight
	// to the summary. If every poli is empty, stopping at the poli list is enough.
	for _, p := range a.polis {
		poli := p
		if err := tt.Click("Poli " + poli.Nama); err != nil {
			t.Fatalf("click poli %q: %v", poli.Nama, err)
		}
		if a.screen == screenSummary {
			t.Logf("ringkasan: poli=%q dokter=%q", a.poli.Nama, a.doctor.NamaDokter)
			if !tt.HasText(poli.Nama) || !tt.HasText(a.doctor.NamaDokter) {
				t.Errorf("summary does not show poli/doctor, texts: %q", tt.Texts())
			}
			if !tt.HasText("Ambil Nomor Antrian") {
				t.Errorf("take-number button is missing, texts: %q", tt.Texts())
			}
			return
		}
		if a.screen != screenDoctor {
			t.Fatalf("screen %d after choosing a poli, want doctor or summary", a.screen)
		}
		// This poli has no doctors today: go back and try another poli.
		clickAll(t, tt, "Ganti Poli")
	}
	t.Skip("no polyclinic has a doctor today")
}

// TestLiveQueueBody sends the body the kiosk builds itself (queueBody), not a
// test-made-up body, to the real server with dry_run=true. This checks what is
// easiest to get wrong: field names, mode, and the "blank without identity"
// rule. No row is written.
func TestLiveQueueBody(t *testing.T) {
	if os.Getenv("APM_LIVE") != "1" {
		t.Skip("set APM_LIVE=1 to test against the real server")
	}
	cfg, path, err := LoadConfig()
	if err != nil {
		t.Fatalf("config %s: %v", path, err)
	}
	if cfg.Mock {
		t.Fatal("config.json still has mock=true")
	}
	api := NewClient(cfg.ServerURL, cfg.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a := newApp(cfg, nil, api)
	tt := newTester(a)

	// New patient path: blank mode, no identity.
	clickAll(t, tt, "Pasien Baru")
	if a.screen != screenPoli {
		t.Fatalf("screen %d, want poli", a.screen)
	}

	// Find a poli that has doctors, then build the body exactly as the kiosk does.
	for _, p := range a.polis {
		poli := p
		docs, err := api.Doctors(ctx, poli.ID)
		if err != nil || len(docs.Dokter) == 0 {
			continue
		}
		a.poli = &poli
		a.doctor = &docs.Dokter[0]

		body := a.queueBody()
		body.DryRun = true
		if body.Mode != "blank" {
			t.Fatalf("mode %q, want blank for a new patient", body.Mode)
		}
		if body.NoRM != "" || body.NIK != "" || body.NoBPJS != "" || body.PetugasID != "" {
			t.Fatalf("blank body carries identity/staff: %+v", body)
		}

		res, err := api.Queue(ctx, body)
		if err != nil {
			t.Logf("poli %q: %v", poli.Nama, err)
			continue
		}
		if !res.DryRun || res.Queue.Kode == "" {
			t.Fatalf("dry-run without a code: %+v", res)
		}
		t.Logf("kiosk body accepted: poli=%q doctor=%q code=%q remaining_quota=%d",
			res.Poli.Nama, res.Dokter.NamaDokter, res.Queue.Kode, res.Queue.SisaKuota)
		return
	}
	t.Skip("no polyclinic has a doctor today")
}

// TestLiveRegistrationBody checks the mode=registration path: identity fields
// and petugas_id must be readable by the server. The RM number is made up, so
// the correct answer is PATIENT_NOT_FOUND (404) — not INVALID_INPUT (400),
// which would mean a wrong field name, and not success, which would mean fake
// data exists.
func TestLiveRegistrationBody(t *testing.T) {
	if os.Getenv("APM_LIVE") != "1" {
		t.Skip("set APM_LIVE=1 to test against the real server")
	}
	cfg, path, err := LoadConfig()
	if err != nil {
		t.Fatalf("config %s: %v", path, err)
	}
	if cfg.Mock {
		t.Fatal("config.json still has mock=true")
	}
	api := NewClient(cfg.ServerURL, cfg.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	polis, err := api.Poli(ctx)
	if err != nil || len(polis) == 0 {
		t.Fatalf("poli: %v", err)
	}

	_, err = api.Queue(ctx, QueueBody{
		Mode:       "registration",
		PoliID:     polis[0].ID,
		PoscheckID: cfg.PoscheckID,
		PetugasID:  cfg.PetugasID,
		NoRM:       "999999999", // made up
		DryRun:     true,
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	switch apiErr.Code {
	case "PATIENT_NOT_FOUND":
		t.Logf("registration fields were read; the invented number was answered with %s", apiErr.Code)
	case "INVALID_INPUT", "PETUGAS_NOT_FOUND", "POSCHECK_NOT_FOUND":
		t.Fatalf("the server rejected the body shape, not the data: %s: %v", apiErr.Code, apiErr)
	default:
		t.Fatalf("unexpected answer for an invented number: %s: %v", apiErr.Code, apiErr)
	}
}
