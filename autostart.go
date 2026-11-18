package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Autostart after a power cut. Windows keeps a short list of programs to start
// at login under HKCU, so the kiosk only has to put its own path there. No
// service, no Administrator rights, no new dependency: advapi32 through
// syscall, the same way printer.go talks to the spooler.
//
// Autostart is not auto-login. If the PC stops at the Windows login screen
// after an outage, nothing is started until somebody logs in; that is a
// Windows setting (netplwiz), not something this app can do.

const (
	// autostartName is the value name inside the Run key. One kiosk per
	// machine, so a fixed name is enough and makes the entry easy to spot.
	autostartName = "SimpusAntrianKiosk"
	// autostartKeyPath is the per-user Run key: no elevation, and it applies
	// to the account the kiosk runs as.
	autostartKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// registry is the slice of the Windows registry this app needs. Narrow on
// purpose: tests use a fake, so `go test` never writes to the machine it runs
// on.
type registry interface {
	// Get returns the value and whether it exists at all.
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
	// Delete is idempotent: removing a value that is not there is not an
	// error, because that is the desired end state.
	Delete(name string) error
}

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSZ           = 1
	regFileNotFound = 2
)

// winRegistry is the real registry, used by main().
type winRegistry struct{}

func openRunKey() (syscall.Handle, error) {
	sub, err := syscall.UTF16PtrFromString(autostartKeyPath)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	code, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)),
		0,
		keyQueryValue|keySetValue,
		uintptr(unsafe.Pointer(&h)),
	)
	if code != 0 {
		return 0, fmt.Errorf("buka registry %s: kode %d", autostartKeyPath, code)
	}
	return h, nil
}

func (winRegistry) Get(name string) (string, bool, error) {
	h, err := openRunKey()
	if err != nil {
		return "", false, err
	}
	defer procRegCloseKey.Call(uintptr(h))

	key, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false, err
	}
	var kind, size uint32
	code, _, _ := procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(key)),
		0,
		uintptr(unsafe.Pointer(&kind)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if code == regFileNotFound {
		return "", false, nil
	}
	if code != 0 {
		return "", false, fmt.Errorf("baca registry %s: kode %d", name, code)
	}
	if size == 0 {
		return "", true, nil
	}

	// size is in bytes, the buffer in UTF-16 units. The extra unit leaves
	// room for a terminator the value may not have.
	buf := make([]uint16, size/2+1)
	code, _, _ = procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(key)),
		0,
		uintptr(unsafe.Pointer(&kind)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if code != 0 {
		return "", false, fmt.Errorf("baca registry %s: kode %d", name, code)
	}
	return syscall.UTF16ToString(buf), true, nil
}

func (winRegistry) Set(name, value string) error {
	h, err := openRunKey()
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(uintptr(h))

	key, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	// UTF16FromString appends the terminator that REG_SZ expects.
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	code, _, _ := procRegSetValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(key)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	if code != 0 {
		return fmt.Errorf("tulis registry %s: kode %d", name, code)
	}
	return nil
}

func (winRegistry) Delete(name string) error {
	h, err := openRunKey()
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(uintptr(h))

	key, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	code, _, _ := procRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(key)))
	if code == regFileNotFound {
		return nil
	}
	if code != 0 {
		return fmt.Errorf("hapus registry %s: kode %d", name, code)
	}
	return nil
}

// exePath is the running executable as an absolute path, which is what the
// Run entry has to contain.
func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}

// isTempPath reports whether exe lives under the temporary folder. A binary
// there disappears the moment the tool that built it cleans up, so pointing
// autostart at it would leave Windows trying to start a file that is gone.
func isTempPath(exe string) bool {
	rel, err := filepath.Rel(os.TempDir(), exe)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// syncAutostart makes the Windows Run entry match config.json. It is a
// reconcile, not a one-time write: moving the app folder or unpacking a new
// zip is fixed by starting the kiosk once, with no extra step for staff.
//
// Nothing here may stop the kiosk. A registry that cannot be written is a log
// line, not a failed start.
func syncAutostart(cfg *Config, reg registry, exe string, lg *Logger) {
	if !cfg.Autostart {
		if err := reg.Delete(autostartName); err != nil {
			lg.Error("autostart tidak bisa dimatikan: %v", err)
		}
		return
	}
	if exe == "" {
		lg.Error("autostart dilewati: path exe tidak diketahui")
		return
	}
	if isTempPath(exe) {
		lg.Error("autostart dilewati: exe berjalan dari folder sementara (%s)", exe)
		return
	}

	// Quoted because the path usually contains spaces, and Windows would
	// otherwise try to run "C:\Program".
	want := `"` + exe + `"`
	have, ok, err := reg.Get(autostartName)
	if err != nil {
		lg.Error("autostart tidak bisa dibaca: %v", err)
		return
	}
	if ok && have == want {
		return
	}
	if err := reg.Set(autostartName, want); err != nil {
		lg.Error("autostart tidak bisa ditulis: %v", err)
		return
	}
	lg.Event("autostart aktif: %s", exe)
}
