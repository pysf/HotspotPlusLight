package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hotspotpluslight/internal/config"
)

// E11.32 --env-file: KEY=VALUE lines, comments, trimming; a real env var wins.
func TestE11_32_EnvFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "hsp.env")
	body := "# comment\r\n\r\n  HSP_DATA_DIR = C:\\Data Dir\\data  \r\nHSP_LOG_FORMAT=\"json\"\nHSP_ADMIN_ADDR=:9000\nHSP_EMPTY=\nHSP_NOTE=abc # a comment\nHSP_HASH=ab#cd\nHSP_QUOTED=\"x # y\"\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	real := func(k string) string {
		if k == "HSP_ADMIN_ADDR" {
			return ":7000"
		}
		return ""
	}
	get, err := envGetter(f, real)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"HSP_DATA_DIR":    `C:\Data Dir\data`,
		"HSP_LOG_FORMAT":  "json",
		"HSP_ADMIN_ADDR":  ":7000", // real env wins
		"HSP_EMPTY":       "",
		"HSP_NOTE":        "abc",
		"HSP_HASH":        "ab#cd",
		"HSP_QUOTED":      "x # y",
		"HSP_NOT_IN_FILE": "",
	} {
		if got := get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if _, err := envGetter(filepath.Join(dir, "missing.env"), real); err == nil {
		t.Error("missing file: want error")
	}
	if _, err := parseEnvFileString("novalue\n"); err == nil {
		t.Error("bad line: want error")
	}
	// plugs into config.Load
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	os.WriteFile(f, []byte("HSP_SECRET_KEY="+key+"\nHSP_DATA_DIR=zz\n"), 0o600)
	get, _ = envGetter(f, func(string) string { return "" })
	cfg, err := config.Load(get)
	if err != nil || cfg.DataDir != "zz" {
		t.Fatalf("config.Load: %v %+v", err, cfg)
	}
}

func TestExtractEnvFile(t *testing.T) {
	rest, p, err := extractEnvFile([]string{"admin", "--env-file", "x.env", "set-password", "--stdin"})
	if err != nil || p != "x.env" || !reflect.DeepEqual(rest, []string{"admin", "set-password", "--stdin"}) {
		t.Fatalf("%v %q %v", rest, p, err)
	}
	rest, p, _ = extractEnvFile([]string{"serve", "--env-file=a b.env"})
	if p != "a b.env" || !reflect.DeepEqual(rest, []string{"serve"}) {
		t.Fatalf("%v %q", rest, p)
	}
	if _, _, err := extractEnvFile([]string{"serve", "--env-file"}); err == nil {
		t.Error("want error")
	}
}

// E11.31 gen-key prints a base64 key that config accepts, and a new one each time.
func TestE11_31_GenKey(t *testing.T) {
	var o1, o2, e bytes.Buffer
	if code := run([]string{"gen-key"}, strings.NewReader(""), &o1, &e); code != 0 {
		t.Fatalf("exit %d: %s", code, e.String())
	}
	run([]string{"gen-key"}, strings.NewReader(""), &o2, &e)
	k := strings.TrimSpace(o1.String())
	if strings.Contains(k, "\n") || k == strings.TrimSpace(o2.String()) {
		t.Fatalf("bad output %q", k)
	}
	if _, err := config.Load(func(n string) string {
		if n == "HSP_SECRET_KEY" {
			return k
		}
		return ""
	}); err != nil {
		t.Fatalf("config rejected the key: %v", err)
	}
}

func parseEnvFileString(s string) (map[string]string, error) {
	return parseEnvFile(newScanner(s))
}

func newScanner(s string) *bufio.Scanner { return bufio.NewScanner(strings.NewReader(s)) }

// TestE16_06_EnvFileQuotedWithComment covers review B1: a quoted value
// followed by " # comment" must be unquoted.
func TestE16_06_EnvFileQuotedWithComment(t *testing.T) {
	m, err := parseEnvFileString("A=\"127.0.0.1:8080\" # local\nB='x # y'   # c\nC=\"a\"\nD=\"unclosed # x\n")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"A": "127.0.0.1:8080", "B": "x # y", "C": "a", "D": `"unclosed`} {
		if m[k] != want {
			t.Errorf("%s = %q, want %q", k, m[k], want)
		}
	}
}

// TestE16_06b_EnvFileQuoteEdgeCases covers quotes inside values, junk after
// the closing quote, unclosed quotes, empty values and '#' inside quotes.
func TestE16_06b_EnvFileQuoteEdgeCases(t *testing.T) {
	for in, want := range map[string]string{
		`K="a"b"`:       `a"b`,
		`K='it's'`:      `it's`,
		`K="x" # c`:     `x`,
		`K="x" junk`:    `"x" junk`,
		`K="unclosed`:   `"unclosed`,
		`K=""`:          ``,
		`K=''  # c`:     ``,
		`K="a # b"`:     `a # b`,
		`K="a # b" # c`: `a # b`,
		`K="x"#c`:       `x`,
	} {
		m, err := parseEnvFileString(in + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if m["K"] != want {
			t.Errorf("%s: got %q, want %q", in, m["K"], want)
		}
	}
}
