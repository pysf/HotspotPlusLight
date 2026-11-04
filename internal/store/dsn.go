package store

import (
	"net/url"
	"runtime"
	"strings"
)

const dsnParams = "_txlock=immediate&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

// readDSN is DSN for the read pool. query_only makes every write fail on those
// connections. (Not mode=ro: WAL readers must still write the -shm file.)
func readDSN(path string) string { return DSN(path) + "&_pragma=query_only(1)" }

// DSN builds the SQLite "file:" URI for a database path. Windows paths
// (drive letter or UNC) are handled on every OS; on Windows relative paths too.
// Backslashes are turned into slashes only when the path looks like a Windows
// path (drive letter or UNC), a drive letter gets a leading "/" (file:///C:/x),
// and every segment is URL-escaped so spaces, '#', '?' and '%' are safe.
func DSN(path string) string { return dsn(path, runtime.GOOS == "windows") }

// dsn is DSN with the host OS as a parameter. On Windows a backslash is a
// separator in every path, also a relative one.
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

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isWindowsPath(p string) bool {
	return (len(p) >= 2 && p[1] == ':' && isLetter(p[0])) || strings.HasPrefix(p, `\\`)
}
