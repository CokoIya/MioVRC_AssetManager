package core

import (
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sync"
)

var Logger *log.Logger

// Logf writes one line to library.log. Addresses, signed links and login data are masked first: the log is
// what a player sends along with feedback.
func Logf(format string, args ...any) {
	if Logger != nil {
		Logger.Print(Scrub(fmt.Sprintf(format, args...)))
	}
}

var (
	reLogHeader = regexp.MustCompile(`(?i)\b((?:set-)?cookie|authorization)\s*[:：]\s*[^\r\n]+`)
	reLogQuery  = regexp.MustCompile(`(https?://[^\s?#"'<>]+)[?#][^\s"'<>]*`)
	reLogMail   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@([A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+)`)
	reLogSecret = regexp.MustCompile(`(?i)\b([a-z0-9_\-]*(?:token|cookie|session|secret|passw(?:or)?d|pwd|signature|auth|bduss|api[_\-]?key)[a-z0-9_\-]*["']?\s*[=:：]\s*["']?)[^\s"'&;,，）)]+`)
	reLogOpaque = regexp.MustCompile(`\b(?:[0-9a-fA-F]{32,}|eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-.]{8,})\b`)
)

// Scrub masks what a log line should not carry: e-mail addresses (the domain stays), the query string of
// links (download links are signed there), cookie and token values.
func Scrub(s string) string {
	s = reLogHeader.ReplaceAllString(s, "$1: ***")
	s = reLogQuery.ReplaceAllString(s, "$1?…")
	s = reLogMail.ReplaceAllString(s, "***@$1")
	s = reLogSecret.ReplaceAllString(s, "$1***")
	return reLogOpaque.ReplaceAllString(s, "***")
}

// logMax: library.log is begun anew when it reaches this size; the one before it stays as library.log.1.
const logMax = 2 << 20

type logFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
	max  int64
}

// OpenLog opens library.log for appending, keeping it and one earlier file under logMax each.
func OpenLog(path string) (io.Writer, error) {
	l := &logFile{path: path, max: logMax}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *logFile) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		l.f = nil
		return err
	}
	l.f, l.size = f, 0
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	return nil
}

func (l *logFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil && l.size > 0 && l.size+int64(len(p)) > l.max {
		l.f.Close()
		moved := os.Rename(l.path, l.path+".1") == nil // replaces the older one
		_ = l.open()
		if !moved {
			l.size = 0 // held open by another copy of the program: go on in it, try again after as much again
		}
	}
	if l.f == nil && l.open() != nil {
		return len(p), nil // the log is not worth failing for
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}
