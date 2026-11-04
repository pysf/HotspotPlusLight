package store

import (
	"net/url"
	"runtime"
	"strings"
)

// dsnParams are the SQLite pragmas of every connection. busy_timeout is only a safety net: writes queue in Go (see Store).
const dsnParams = "_txlock=immediate&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

// readDSN is DSN for the read pool. query_only makes every write fail on those
// connections. (Not mode=ro: WAL readers must still write the -shm file.)
func readDSN(path string) string { return DSN(path) + "&_pragma=query_only(1)" }

// DSN returns the SQLite "file:" URI for a database path, with the pragmas.
// It accepts Windows paths (drive letter or UNC) on every OS, and escapes each
// segment so spaces, '#', '?' and '%' are safe.
func DSN(path string) string { return dsn(path, runtime.GOOS == "windows") }

// dsn is DSN with the host OS as a parameter, so tests can cover Windows paths on any OS.
func dsn(path string, onWindows bool) string {
	p := path
	if onWindows || isWindowsPath(p) {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	if len(p) >= 2 && p[1] == ':' && isLetter(p[0]) {
		p = "/" + p
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	p = strings.Join(segs, "/")
	if strings.HasPrefix(p, "/") {
		return "file://" + p + "?" + dsnParams
	}
	return "file:" + p + "?" + dsnParams
}

// isLetter reports whether b is an ASCII letter.
func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// isWindowsPath reports whether p starts with a drive letter or is a UNC path.
func isWindowsPath(p string) bool {
	return (len(p) >= 2 && p[1] == ':' && isLetter(p[0])) || strings.HasPrefix(p, `\\`)
}
