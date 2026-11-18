package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeRegistry keeps the Run key in memory, so the tests never touch the
// registry of the machine running them.
type fakeRegistry struct {
	values map[string]string
	// failGet/failSet/failDelete inject failures; autostart must survive all
	// three without stopping the kiosk.
	failGet, failSet, failDelete bool
	sets, deletes                int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{values: map[string]string{}}
}

func (f *fakeRegistry) Get(name string) (string, bool, error) {
	if f.failGet {
		return "", false, errors.New("registry tidak terbaca")
	}
	v, ok := f.values[name]
	return v, ok, nil
}

func (f *fakeRegistry) Set(name, value string) error {
	if f.failSet {
		return errors.New("registry tidak bisa ditulis")
	}
	f.sets++
	f.values[name] = value
	return nil
}

func (f *fakeRegistry) Delete(name string) error {
	if f.failDelete {
		return errors.New("registry tidak bisa dihapus")
	}
	f.deletes++
	delete(f.values, name)
	return nil
}

func autostartConfig(on bool) *Config {
	return &Config{Mock: true, Autostart: on}
}

// The normal path: autostart on writes the quoted exe path, and a second start
// with the same path writes nothing. Rewriting on every boot would be noise in
// the registry and in the log.
func TestAutostartWritesExePathOnce(t *testing.T) {
	reg := newFakeRegistry()
	exe := `C:\kiosk\simpus-antrian-interactive.exe`

	syncAutostart(autostartConfig(true), reg, exe, nil)
	if got := reg.values[autostartName]; got != `"`+exe+`"` {
		t.Fatalf("registry value %q, want the quoted exe path", got)
	}
	if reg.sets != 1 {
		t.Fatalf("wrote %d times, want 1", reg.sets)
	}

	syncAutostart(autostartConfig(true), reg, exe, nil)
	if reg.sets != 1 {
		t.Errorf("an unchanged path must not be written again, sets=%d", reg.sets)
	}
}

// Moving the app folder must repair the entry, not leave Windows pointing at a
// path that no longer exists.
func TestAutostartUpdatesMovedPath(t *testing.T) {
	reg := newFakeRegistry()
	reg.values[autostartName] = `"C:\lama\simpus-antrian-interactive.exe"`

	syncAutostart(autostartConfig(true), reg, `C:\baru\simpus-antrian-interactive.exe`, nil)

	if got := reg.values[autostartName]; got != `"C:\baru\simpus-antrian-interactive.exe"` {
		t.Errorf("registry value %q, want the new path", got)
	}
}

// autostart false means the entry is gone, even when it was never there: the
// desired end state is "not in the Run key", and a missing value already is.
func TestAutostartOffRemovesEntry(t *testing.T) {
	reg := newFakeRegistry()
	reg.values[autostartName] = `"C:\kiosk\simpus-antrian-interactive.exe"`

	syncAutostart(autostartConfig(false), reg, `C:\kiosk\simpus-antrian-interactive.exe`, nil)
	if _, ok := reg.values[autostartName]; ok {
		t.Error("autostart false must remove the Run entry")
	}

	// A second start must not fail on a value that is already absent.
	syncAutostart(autostartConfig(false), reg, `C:\kiosk\simpus-antrian-interactive.exe`, nil)
	if reg.deletes != 2 {
		t.Errorf("deletes=%d, want 2 (Delete is idempotent)", reg.deletes)
	}
}

// A binary in the temp folder disappears when the tool that built it cleans
// up, so pointing autostart at it would leave Windows starting nothing.
func TestAutostartSkipsTempFolder(t *testing.T) {
	reg := newFakeRegistry()
	exe := tempPathForTest(t)

	syncAutostart(autostartConfig(true), reg, exe, nil)
	if _, ok := reg.values[autostartName]; ok {
		t.Errorf("temp path %q must not be registered", exe)
	}
	if reg.sets != 0 {
		t.Errorf("wrote %d times for a temp path, want 0", reg.sets)
	}
}

// Turning autostart off from a temp build must still work: the guard protects
// against registering a doomed path, not against cleaning up.
func TestAutostartOffStillClearsFromTempFolder(t *testing.T) {
	reg := newFakeRegistry()
	reg.values[autostartName] = `"C:\kiosk\simpus-antrian-interactive.exe"`

	syncAutostart(autostartConfig(false), reg, tempPathForTest(t), nil)
	if _, ok := reg.values[autostartName]; ok {
		t.Error("autostart false must clear the entry even from a temp build")
	}
}

// A registry that refuses to cooperate is a log line, never a failed start.
func TestAutostartSurvivesRegistryFailures(t *testing.T) {
	exe := `C:\kiosk\simpus-antrian-interactive.exe`

	for _, c := range []struct {
		name string
		reg  *fakeRegistry
	}{
		{"get", &fakeRegistry{values: map[string]string{}, failGet: true}},
		{"set", &fakeRegistry{values: map[string]string{}, failSet: true}},
	} {
		reg := c.reg
		syncAutostart(autostartConfig(true), reg, exe, nil) // must not panic
	}

	off := &fakeRegistry{values: map[string]string{}, failDelete: true}
	syncAutostart(autostartConfig(false), off, exe, nil) // must not panic
}

// An unknown exe path cannot be registered, and guessing one would be worse
// than doing nothing.
func TestAutostartNeedsExePath(t *testing.T) {
	reg := newFakeRegistry()
	syncAutostart(autostartConfig(true), reg, "", nil)
	if _, ok := reg.values[autostartName]; ok {
		t.Error("an empty exe path must not be registered")
	}
}

// A path outside the temp folder must never be mistaken for one inside it,
// including folders whose name merely starts with the temp folder's name.
func TestIsTempPath(t *testing.T) {
	dir := strings.TrimRight(os.TempDir(), `\/`)
	inside := dir + `\simpus-antrian-interactive.exe`
	if !isTempPath(inside) {
		t.Errorf("%q should be recognised as temporary", inside)
	}
	if isTempPath(`C:\kiosk\simpus-antrian-interactive.exe`) {
		t.Error("a normal folder must not be treated as temporary")
	}
	sibling := dir + `-lain\simpus-antrian-interactive.exe`
	if isTempPath(sibling) {
		t.Errorf("%q shares the temp prefix but is not inside it", sibling)
	}
}

// tempPathForTest builds a path inside the real temp folder, so isTempPath is
// tested against the same value it compares with at runtime.
func tempPathForTest(t *testing.T) string {
	t.Helper()
	dir := strings.TrimRight(os.TempDir(), `\/`)
	return dir + `\simpus-antrian-interactive.exe`
}
