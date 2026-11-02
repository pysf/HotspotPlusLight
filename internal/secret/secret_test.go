package secret

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func box(t *testing.T, fill byte) *Box {
	b, err := NewBox(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTripAndKeyCheck(t *testing.T) {
	a, other := box(t, 1), box(t, 2)
	enc, _ := a.Encrypt([]byte("hello"))
	if bytes.Contains(enc, []byte("hello")) {
		t.Fatal("ciphertext contains plaintext")
	}
	if p, err := a.Decrypt(enc); err != nil || string(p) != "hello" {
		t.Fatalf("decrypt: %q %v", p, err)
	}
	kc, _ := a.KeyCheck()
	if err := a.VerifyKeyCheck(kc); err != nil {
		t.Fatal(err)
	}
	if err := other.VerifyKeyCheck(kc); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey(base64.StdEncoding.EncodeToString(make([]byte, 32))); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestRandomDigits(t *testing.T) {
	a, err := RandomDigits(6)
	if err != nil || len(a) != 6 {
		t.Fatalf("%q %v", a, err)
	}
	for _, r := range a {
		if r < '0' || r > '9' {
			t.Fatalf("non-digit in %q", a)
		}
	}
}

func TestRandomDigitsBounds(t *testing.T) {
	for _, n := range []int{1, 4, 20, MaxRandomDigits} {
		a, err := RandomDigits(n)
		if err != nil || len(a) != n || strings.Trim(a, "0123456789") != "" {
			t.Fatalf("n=%d: %q %v", n, a, err)
		}
	}
	for _, n := range []int{-1, 0, MaxRandomDigits + 1} {
		if _, err := RandomDigits(n); err == nil {
			t.Errorf("n=%d accepted", n)
		}
	}
}
