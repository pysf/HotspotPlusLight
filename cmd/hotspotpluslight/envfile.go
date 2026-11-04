package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseEnvFile reads KEY=VALUE lines. Empty lines and lines starting with '#'
// are skipped. Keys and values are trimmed. A value may be wrapped in one pair
// of single or double quotes. In an unquoted value, a space followed by '#'
// starts a comment that is removed (a '#' with no space before it is kept).
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
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i]) // unquoted value: drop a trailing " #comment"
		}
		m[k] = v
	}
	return m, r.Err()
}

// envGetter returns a getenv function. Without a file it is os.Getenv. With a
// file, a real (non-empty) environment variable wins over the file.
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

// extractEnvFile removes "--env-file <path>" or "--env-file=<path>" from args.
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
