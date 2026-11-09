package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseEnvFile returns the KEY=VALUE pairs read from r. It skips empty lines
// and lines that start with '#', and accepts values in one pair of quotes,
// optionally followed by a comment. In an unquoted value, " #" starts a comment.
// It fails on a line without a key.
func parseEnvFile(r *bufio.Scanner) (map[string]string, error) {
	m := map[string]string{}
	n := 0
	for r.Scan() {
		n++
		line := strings.TrimSpace(strings.TrimPrefix(r.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", n)
		}
		v = strings.TrimSpace(v)
		if q := quotedValue(v); q != "" {
			v = q[1 : len(q)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i]) // unquoted value: drop a trailing " #comment"
		}
		m[k] = v
	}
	return m, r.Err()
}

// quotedValue returns the quoted part (with its quotes) at the start of v. The
// closing quote is the last matching quote that is followed by nothing or by
// a "# comment". It returns "" when there is none, so text like `"x" junk` stays
// raw and visible instead of being cut silently.
func quotedValue(v string) string {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return ""
	}
	for j := len(v) - 1; j > 0; j-- {
		if v[j] != v[0] {
			continue
		}
		if rest := strings.TrimSpace(v[j+1:]); rest == "" || strings.HasPrefix(rest, "#") {
			return v[:j+1]
		}
	}
	return ""
}

// envGetter returns a getenv function for the env file at path. An empty path
// returns real. A non-empty environment variable wins over the file.
func envGetter(path string, real func(string) string) (func(string) string, error) {
	if path == "" {
		return real, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("env file: %w", err)
	}
	defer f.Close()
	m, err := parseEnvFile(bufio.NewScanner(f))
	if err != nil {
		return nil, fmt.Errorf("env file %s: %w", path, err)
	}
	return func(k string) string {
		if v := real(k); v != "" {
			return v
		}
		return m[k]
	}, nil
}

// extractEnvFile returns args without the --env-file option, and its path (empty
// if absent). It fails when the option has no path.
func extractEnvFile(args []string) (rest []string, path string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--env-file":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--env-file needs a path")
			}
			path = args[i+1]
			i++
		case strings.HasPrefix(a, "--env-file="):
			path = strings.TrimPrefix(a, "--env-file=")
		default:
			rest = append(rest, a)
		}
	}
	return rest, path, nil
}
