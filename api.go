package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Data shapes from simpus-antrian-server. JSON fields follow `CONTRACT.md`
// §2.9 as-is; do not change them unilaterally.

type Patient struct {
	NoRM         string `json:"no_rm"`
	Nama         string `json:"nama"`
	Alamat       string `json:"alamat"`
	Desa         string `json:"desa"`
	TanggalLahir string `json:"tanggal_lahir"`
	NoBPJS       string `json:"no_bpjs"`
	NIK          string `json:"nik"`
	IsBPJS       bool   `json:"is_bpjs"`
}

type BPJSStatus struct {
	IsBPJS        bool   `json:"is_bpjs"`
	NoKartu       string `json:"no_kartu"`
	Sumber        string `json:"sumber"`
	Terverifikasi bool   `json:"terverifikasi"`
	Aktif         *bool  `json:"aktif"`
	Status        string `json:"status"`
	JenisPeserta  string `json:"jenis_peserta"`
	Kelas         string `json:"kelas"`
	NamaProvider  string `json:"nama_provider"`
	Pesan         string `json:"pesan"`
}

type Poli struct {
	ID     string `json:"id"`
	Nama   string `json:"nama"`
	Prefix string `json:"prefix"`
}

type Doctor struct {
	KodeDokter string `json:"kode_dokter"`
	NamaDokter string `json:"nama_dokter"`
	JamPraktek string `json:"jam_praktek"`
	Kapasitas  int    `json:"kapasitas"`
}

type Queue struct {
	Kode          string `json:"kode"`
	NoUrutPoli    int    `json:"no_urut_poli"`
	Tanggal       string `json:"tanggal"`
	Waktu         string `json:"waktu"`
	Poli          string `json:"poli"`
	Dokter        string `json:"dokter"`
	MenungguAhead int    `json:"menunggu_di_depan"`
	SisaKuota     int    `json:"sisa_kuota"`
}

type OptionRef struct {
	ID   string `json:"id"`
	Nama string `json:"nama"`
}

type OptionSelection struct {
	PetugasID  string `json:"petugas_id"`
	PoscheckID string `json:"poscheck_id"`
	Lokasi     string `json:"lokasi"`
}

type OptionsResult struct {
	Petugas  []OptionRef     `json:"petugas"`
	Poscheck []OptionRef     `json:"poscheck"`
	Terpilih OptionSelection `json:"terpilih"`
}

type LookupResult struct {
	Mode        string      `json:"mode"`
	CanRegister bool        `json:"can_register"`
	Fallback    string      `json:"fallback"`
	Reason      string      `json:"reason"`
	Patient     *Patient    `json:"patient"`
	BPJS        *BPJSStatus `json:"bpjs"`
}

type DoctorList struct {
	Poli     Poli     `json:"poli"`
	Dokter   []Doctor `json:"dokter"`
	Otomatis bool     `json:"otomatis"`
	Dipilih  *Doctor  `json:"dipilih"`
}

type QueueResult struct {
	Mode           string      `json:"mode"`
	Patient        *Patient    `json:"patient"`
	BPJS           *BPJSStatus `json:"bpjs"`
	Poli           Poli        `json:"poli"`
	Dokter         Doctor      `json:"dokter"`
	Queue          Queue       `json:"antrian"`
	Lokasi         string      `json:"lokasi"`
	DryRun         bool        `json:"dry_run"`
	SudahTerdaftar bool        `json:"sudah_terdaftar"`
}

// APIError is a domain error from the server, complete with code and HTTP
// status, so screens can pick a reaction without parsing the message string.
type APIError struct {
	Status  int
	Code    string
	Message string
	Data    json.RawMessage
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Message)
}

// Retryable marks errors worth retrying: server busy or database briefly
// disconnected. Beyond these, retrying will not change the outcome.
func (e *APIError) Retryable() bool {
	return e.Code == "QUEUE_BUSY" || e.Code == "DB_UNAVAILABLE"
}

// Client is the only door to simpus-antrian-server.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Options(ctx context.Context) (*OptionsResult, error) {
	var out OptionsResult
	if err := c.do(ctx, http.MethodGet, "/api/v1/apm/options", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Lookup finds a patient. `pcare=false` is used on the identification screen
// for speed; the real BPJS check happens when the queue is created.
func (c *Client) Lookup(ctx context.Context, identType, value string, pcare bool) (*LookupResult, error) {
	body := map[string]any{pcareFieldName: pcare}
	body[identType] = value
	var out LookupResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/apm/lookup", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Poli(ctx context.Context) ([]Poli, error) {
	var out []Poli
	if err := c.do(ctx, http.MethodGet, "/api/v1/apm/poli", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Doctors(ctx context.Context, poliID string) (*DoctorList, error) {
	var out DoctorList
	q := url.Values{"poli_id": {poliID}}
	if err := c.do(ctx, http.MethodGet, "/api/v1/apm/dokter", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QueueBody is the body of `POST /api/v1/apm/queue`.
type QueueBody struct {
	Mode       string `json:"mode"`
	NIK        string `json:"nik,omitempty"`
	NoBPJS     string `json:"no_bpjs,omitempty"`
	NoRM       string `json:"no_rm,omitempty"`
	PoliID     string `json:"poli_id"`
	KodeDokter string `json:"kode_dokter,omitempty"`
	PetugasID  string `json:"petugas_id,omitempty"`
	PoscheckID string `json:"poscheck_id,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
	PCare      bool   `json:"pcare"`
}

func (c *Client) Queue(ctx context.Context, body QueueBody) (*QueueResult, error) {
	var out QueueResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/apm/queue", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

const pcareFieldName = "pcare"

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{Status: resp.StatusCode, Code: "BAD_RESPONSE", Message: "respons server tidak dikenali"}
	}

	if env.Error != nil {
		return &APIError{
			Status:  resp.StatusCode,
			Code:    env.Error.Code,
			Message: env.Error.Message,
			Data:    env.Data,
		}
	}
	if resp.StatusCode >= 400 {
		return &APIError{Status: resp.StatusCode, Code: "HTTP_" + http.StatusText(resp.StatusCode), Message: http.StatusText(resp.StatusCode)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &APIError{Status: resp.StatusCode, Code: "BAD_RESPONSE", Message: "bentuk data server berubah"}
	}
	return nil
}

// ModeForLookup translates a lookup result into a queue mode, following the
// table in `CONTRACT.md` §4. Do not guess elsewhere.
func ModeForLookup(r *LookupResult) string {
	switch r.Mode {
	case "existing_active":
		return "registration"
	case "existing_inactive", "new":
		return "blank"
	case "existing_bpjs_unknown":
		if r.CanRegister {
			return "registration"
		}
		return "blank"
	}
	return "blank"
}

// BPJSVerdict condenses BPJS status into three possibilities for screen 6.
type BPJSVerdict int

const (
	BPJSUnknown BPJSVerdict = iota
	BPJSActive
	BPJSInactive
)

func VerdictOf(b *BPJSStatus) BPJSVerdict {
	if b == nil || !b.Terverifikasi || b.Aktif == nil {
		return BPJSUnknown
	}
	if *b.Aktif {
		return BPJSActive
	}
	return BPJSInactive
}

// errorText translates an error into a sentence a patient can read.
// Raw server technical messages are never shown in the kiosk.
func errorText(err error) string {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return "Tidak bisa menghubungi server. Silakan panggil petugas."
	}
	switch apiErr.Code {
	case "UNAUTHORIZED", "APM_NOT_CONFIGURED":
		return "Kiosk belum terhubung ke server. Silakan panggil petugas."
	case "QUOTA_EXCEEDED":
		return "Kuota dokter hari ini sudah penuh. Silakan pilih dokter lain atau hubungi petugas."
	case "POLI_NOT_FOUND", "DOCTOR_NOT_FOUND":
		return "Pilihan poli atau dokter sudah tidak tersedia. Silakan pilih ulang."
	case "DOCTOR_REQUIRED":
		return "Pilih dokter terlebih dahulu."
	case "BPJS_INACTIVE":
		return "Kartu BPJS tidak aktif. Pendaftaran dilanjutkan di loket."
	case "BPJS_INCOMPLETE", "BPJS_DOB_MISMATCH", "BPJS_POLI_MISSING", "PCARE_UNAVAILABLE":
		return "Data BPJS belum bisa diperiksa. Silakan daftar di loket."
	case "QUEUE_BUSY", "DB_UNAVAILABLE":
		return "Server sedang sibuk. Silakan coba lagi sebentar."
	case "IDENTIFIER_MISMATCH":
		return "Nomor yang dimasukkan tidak cocok dengan data pasien. Silakan panggil petugas."
	case "PATIENT_NOT_FOUND":
		return "Data pasien tidak ditemukan. Silakan pilih Pasien Baru."
	}
	// PETUGAS_NOT_FOUND, POSCHECK_NOT_FOUND, INVALID_INPUT, and the rest are
	// misconfiguration or bugs, not patient mistakes: do not ask them to fix
	// anything.
	return "Terjadi kesalahan. Silakan panggil petugas."
}

// IsRetryable reports whether an error is worth retrying (2-3 times) before
// sending the patient to staff.
func IsRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	// Network errors have no code; treat them as worth retrying.
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) {
		return true
	}
	return err != nil && !errors.As(err, &apiErr)
}
