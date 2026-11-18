package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Windows RAW printing through the spooler. This is how ESC/POS bytes reach a
// USB thermal printer that Windows already knows about: the spooler hands the
// bytes to the driver untouched, so no ESC/POS library and no extra dependency
// is needed.
//
// The printer must be installed as a Windows printer, with its vendor driver.
// Opening the USB device directly would bypass that driver and needs the
// device path, which differs per model and cable.

var (
	winspool = syscall.NewLazyDLL("winspool.drv")

	procOpenPrinterW     = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
)

// docInfo1 mirrors DOC_INFO_1W.
type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

// rawPrinter sends bytes to one named Windows printer.
type rawPrinter struct {
	name string

	// mu serialises jobs. Two tickets printing at once would interleave
	// their bytes on the same printer.
	mu sync.Mutex
}

func newRawPrinter(name string) *rawPrinter {
	return &rawPrinter{name: name}
}

func (p *rawPrinter) String() string { return p.name }

// Print writes job to the printer copies times. Datatype "RAW" is what makes
// the spooler pass the bytes through untouched.
func (p *rawPrinter) Print(job []byte, copies int) error {
	if copies < 1 {
		copies = 1
	}
	if len(job) == 0 {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	namePtr, err := syscall.UTF16PtrFromString(p.name)
	if err != nil {
		return fmt.Errorf("nama printer %q: %w", p.name, err)
	}
	docPtr, err := syscall.UTF16PtrFromString("Antrian")
	if err != nil {
		return err
	}
	rawPtr, err := syscall.UTF16PtrFromString("RAW")
	if err != nil {
		return err
	}

	var handle syscall.Handle
	if ok, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&handle)),
		0,
	); ok == 0 {
		// The usual cause is a printer name that does not match Windows
		// exactly. Saying so here saves staff from chasing a cable.
		return fmt.Errorf("printer %q tidak bisa dibuka (cek nama printer di Windows): %w", p.name, callErr)
	}
	defer procClosePrinter.Call(uintptr(handle))

	info := docInfo1{pDocName: docPtr, pDatatype: rawPtr}
	if ok, _, callErr := procStartDocPrinterW.Call(
		uintptr(handle),
		1,
		uintptr(unsafe.Pointer(&info)),
	); ok == 0 {
		return fmt.Errorf("mulai dokumen di %q: %w", p.name, callErr)
	}
	// EndDocPrinter must run even when writing fails, or the spooler keeps
	// the job open and the next ticket queues behind a dead one.
	defer procEndDocPrinter.Call(uintptr(handle))

	if ok, _, callErr := procStartPagePrinter.Call(uintptr(handle)); ok == 0 {
		return fmt.Errorf("mulai halaman di %q: %w", p.name, callErr)
	}
	defer procEndPagePrinter.Call(uintptr(handle))

	for i := 0; i < copies; i++ {
		var written uint32
		if ok, _, callErr := procWritePrinter.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&job[0])),
			uintptr(len(job)),
			uintptr(unsafe.Pointer(&written)),
		); ok == 0 {
			return fmt.Errorf("tulis ke %q: %w", p.name, callErr)
		}
		if int(written) != len(job) {
			return fmt.Errorf("tulis ke %q: %d dari %d byte", p.name, written, len(job))
		}
	}
	return nil
}

// printerFromConfig builds the configured printer, or nil when printing is
// off. An empty name is how printing is switched off: there is no separate
// switch to fall out of step with it.
func printerFromConfig(cfg *Config) printer {
	if cfg == nil {
		return nil
	}
	name := cfg.Printer.Name
	if name == "" {
		return nil
	}
	return newRawPrinter(name)
}
