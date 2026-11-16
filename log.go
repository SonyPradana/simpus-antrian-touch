package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes one file per day in the log/ folder so the file does not grow
// without bound and is easy to send to an admin when something breaks.
type Logger struct {
	dir string

	mu   sync.Mutex
	day  string
	file *os.File
}

func NewLogger(dir string) *Logger {
	return &Logger{dir: dir}
}

// Event records normal events (queue number, poli, doctor, no RM).
func (l *Logger) Event(format string, args ...any) {
	l.write("INFO", format, args...)
}

// Error records failures, including the error code from the server.
func (l *Logger) Error(format string, args ...any) {
	l.write("ERROR", format, args...)
}

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

func (l *Logger) write(level, format string, args ...any) {
	if l == nil {
		return
	}
	now := time.Now()
	line := fmt.Sprintf("%s %-5s %s\n", now.Format("2006-01-02 15:04:05"), level, fmt.Sprintf(format, args...))

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureFile(now); err != nil {
		return
	}
	l.file.WriteString(line)
}

// ensureFile opens today's file, and closes yesterday's file when the date
// rolls over — a kiosk app stays on for days.
func (l *Logger) ensureFile(now time.Time) error {
	day := now.Format("2006-01-02")
	if l.file != nil && l.day == day {
		return nil
	}
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, "kiosk-"+day+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.file, l.day = f, day
	return nil
}
