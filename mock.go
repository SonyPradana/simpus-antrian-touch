package main

import (
	"context"
	"errors"
	"fmt"
)

// mockAPI uses made-up data so screens can be developed without a server
// and without a database. There is no real patient data here.
type mockAPI struct{}

func newMockAPI() *mockAPI { return &mockAPI{} }

var mockPolis = []Poli{
	{ID: "01", Nama: "Poli Umum", Prefix: "A"},
	{ID: "02", Nama: "Poli Gigi", Prefix: "B"},
	{ID: "03", Nama: "Poli KIA", Prefix: "C"},
	{ID: "04", Nama: "Poli Gizi", Prefix: "D"},
}

var mockDoctors = map[string][]Doctor{
	"01": {
		{KodeDokter: "00001", NamaDokter: "dr. Contoh Satu", JamPraktek: "08.00–14.00", Kapasitas: 20},
		{KodeDokter: "00002", NamaDokter: "dr. Contoh Dua", JamPraktek: "14.00–20.00", Kapasitas: 12},
	},
	"02": {
		{KodeDokter: "00003", NamaDokter: "drg. Contoh Tiga", JamPraktek: "08.00–12.00", Kapasitas: 8},
	},
	"03": {
		{KodeDokter: "00004", NamaDokter: "dr. Contoh Empat", JamPraktek: "08.00–13.00", Kapasitas: 15},
	},
	"04": {},
}

func (m *mockAPI) Lookup(_ context.Context, identType, value string, _ bool) (*LookupResult, error) {
	switch len(CleanIdentifier(value)) {
	case 0:
		return nil, errors.New("identitas kosong")
	case 6:
		aktif := true
		return &LookupResult{
			Mode:        "existing_active",
			CanRegister: true,
			Patient: &Patient{
				NoRM:         value,
				Nama:         "Pasien Contoh",
				Alamat:       "Dusun Contoh",
				Desa:         "Desa Contoh",
				TanggalLahir: "1970-01-01",
				IsBPJS:       true,
			},
			BPJS: &BPJSStatus{IsBPJS: true, Terverifikasi: true, Aktif: &aktif, NoKartu: "0000000000001"},
		}, nil
	default:
		// Long numbers count as new patients, like the server's `new` mode.
		return &LookupResult{Mode: "new", Fallback: "blank", Reason: "patient_not_found"}, nil
	}
}

func (m *mockAPI) Poli(context.Context) ([]Poli, error) { return mockPolis, nil }

func (m *mockAPI) Doctors(_ context.Context, poliID string) (*DoctorList, error) {
	list := mockDoctors[poliID]
	out := &DoctorList{Dokter: list}
	for _, p := range mockPolis {
		if p.ID == poliID {
			out.Poli = p
		}
	}
	if len(list) == 1 {
		out.Otomatis = true
		out.Dipilih = &list[0]
	}
	return out, nil
}

func (m *mockAPI) Queue(_ context.Context, body QueueBody) (*QueueResult, error) {
	poli := Poli{ID: body.PoliID}
	for _, p := range mockPolis {
		if p.ID == body.PoliID {
			poli = p
		}
	}
	var doctor Doctor
	for _, d := range mockDoctors[body.PoliID] {
		if d.KodeDokter == body.KodeDokter {
			doctor = d
		}
	}
	res := &QueueResult{
		Mode:   body.Mode,
		Poli:   poli,
		Dokter: doctor,
		Queue: Queue{
			Kode:          fmt.Sprintf("%s%03d", poli.Prefix, 7),
			NoUrutPoli:    7,
			Poli:          poli.Nama,
			Dokter:        doctor.NamaDokter,
			MenungguAhead: 3,
			SisaKuota:     doctor.Kapasitas - 7,
		},
	}
	if body.Mode == "registration" {
		res.Patient = &Patient{NoRM: body.NoRM, Nama: "Pasien Contoh"}
	}
	return res, nil
}
