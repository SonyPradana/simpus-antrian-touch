package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// utf8BOM is the byte order mark that Notepad and PowerShell add when writing
// UTF-8. JSON does not allow it.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Config is read from config.json next to the exe. Every value has a default
// so the kiosk can still open even if the file is incomplete.
type Config struct {
	ServerURL  string `json:"server_url"`
	APIKey     string `json:"api_key"`
	PoscheckID string `json:"poscheck_id"`
	PetugasID  string `json:"petugas_id"`
	// Mock uses made-up data in memory, to develop screens without a server
	// and without a database.
	Mock bool `json:"mock"`
	// Windowed uses a normal window that can be resized and closed. For
	// development only; default false means full-screen kiosk.
	Windowed bool `json:"windowed"`
	// DryRun sends dry_run=true to the server: the queue number is computed,
	// but no row is written to the database. Used when testing the kiosk
	// against the production server. Default false = take a real number.
	DryRun bool `json:"dry_run"`
	// Printer configures the receipt printer. Empty (the default) means no
	// printer: the number still shows on screen, nothing is printed, and no
	// error is reported.
	Printer PrinterConfig `json:"printer"`
}

// PrinterConfig points at one ESC/POS printer. Printing is best effort: the
// kiosk never fails or blocks because a printer is unreachable.
type PrinterConfig struct {
	// Name is the printer's name in Windows, character for character. Empty
	// means printing is off, which is the default: no paper, no log noise.
	// `Get-Printer | Select-Object Name` lists the candidates.
	Name string `json:"name"`
	// Width is the receipt width in characters. 32 for 58 mm, 48 for 80 mm.
	Width int `json:"width"`
	// Copies is how many times the receipt is printed. Default 1.
	Copies int `json:"copies"`
}

// printerWidth normalises the receipt width. An unset or absurd value falls
// back to 32 characters (58 mm paper), the most common ticket size.
func (p *PrinterConfig) printerWidth() int {
	switch {
	case p.Width == 0:
		return 32
	case p.Width < 20 || p.Width > 96:
		return 32
	default:
		return p.Width
	}
}

// printerCopies normalises the copy count. Zero or less means one copy.
func (p *PrinterConfig) printerCopies() int {
	if p.Copies < 1 {
		return 1
	}
	return p.Copies
}

const (
	defaultServerURL = "http://localhost:8080"
	configFileName   = "config.json"
)

// LoadConfig reads config.json. Search order: CONFIG_FILE, exe folder,
// then the working folder (so `go run` and `go tool mygo dev` also work).
func LoadConfig() (*Config, string, error) {
	path, err := configPath()
	if err != nil {
		return nil, path, err
	}
	if env := strings.TrimSpace(os.Getenv("CONFIG_FILE")); env != "" && path == env {
		// An explicit CONFIG_FILE that does not exist must say so. Falling
		// through to the defaults would report "api_key is empty", which
		// sends staff looking at the file instead of at the path.
		if _, err := os.Stat(env); err != nil {
			return nil, env, fmt.Errorf("CONFIG_FILE tidak ditemukan: %s: %w", env, err)
		}
	}
	cfg := &Config{}
	var notFound string
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		// Windows Notepad writes UTF-8 with a BOM, and json.Unmarshal
		// rejects it. Staff will edit this file, so the BOM is dropped
		// silently rather than the kiosk refusing to start over something
		// they cannot see.
		data = bytes.TrimPrefix(data, utf8BOM)
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, path, fmt.Errorf("config.json rusak: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// New kiosk: use defaults, do not fail just because the file is absent.
		// Validation below still refuses mock=false with no key, and says
		// where it looked so the file is easy to put in the right place.
		notFound = path
	default:
		return nil, path, err
	}
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		if notFound != "" {
			return nil, path, fmt.Errorf("%w (config.json tidak ditemukan di %s; salin config.example.json ke sana)", err, notFound)
		}
		return nil, path, err
	}
	return cfg, path, nil
}

func configPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("CONFIG_FILE")); p != "" {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), configFileName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, configFileName), nil
	}
	return configFileName, nil
}

// exeDir is the folder the running executable lives in. Feeding log files
// through the current directory instead would drop them into wherever the
// shortcut or Task Scheduler happened to start us.
func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func (c *Config) normalize() {
	c.ServerURL = strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	if c.ServerURL == "" {
		c.ServerURL = defaultServerURL
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.PoscheckID = strings.TrimSpace(c.PoscheckID)
	c.PetugasID = strings.TrimSpace(c.PetugasID)
	// A trailing space in the printer name is invisible in a text editor but
	// makes OpenPrinter fail, so it is dropped here.
	c.Printer.Name = strings.TrimSpace(c.Printer.Name)
}

func (c *Config) validate() error {
	if c.Mock {
		return nil
	}
	if c.APIKey == "" {
		return errors.New("api_key kosong: isi config.json atau set mock=true")
	}
	if c.PoscheckID == "" {
		return errors.New("poscheck_id kosong: isi config.json")
	}
	return nil
}
