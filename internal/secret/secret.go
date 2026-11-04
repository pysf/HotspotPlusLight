// Package secret provides AES-256-GCM encryption for stored member passwords
// and the settings.key_check value.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeyCheckPlaintext is the fixed string encrypted into settings.key_check.
const KeyCheckPlaintext = "hotspotpluslight-key-check-v1"

// Box encrypts and decrypts with one AES-256-GCM key.
type Box struct{ gcm cipher.AEAD }

// ParseKey returns the key decoded from HSP_SECRET_KEY. It fails if the value is
// not base64 or not exactly 32 bytes.
func ParseKey(b64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("must decode to exactly 32 bytes, got %d", len(key))
	}
	return key, nil
}

// NewBox returns a Box for key, which must be 32 bytes.
func NewBox(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt returns plain encrypted with a new random nonce placed in front.
func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.gcm.Seal(nonce, nonce, plain, nil), nil
}

// Decrypt returns the plain text of data made by Encrypt. It fails on short or
// altered data and on a wrong key.
func (b *Box) Decrypt(data []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(data) < n {
		return nil, errors.New("ciphertext too short")
	}
	return b.gcm.Open(nil, data[:n], data[n:], nil)
}

// KeyCheck returns the text stored in settings.key_check.
func (b *Box) KeyCheck() (string, error) {
	enc, err := b.Encrypt([]byte(KeyCheckPlaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// VerifyKeyCheck returns an error if stored was not made with this box's key.
func (b *Box) VerifyKeyCheck(stored string) error {
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return errors.New("stored key_check is corrupt")
	}
	plain, err := b.Decrypt(raw)
	if err != nil || string(plain) != KeyCheckPlaintext {
		return errors.New("HSP_SECRET_KEY cannot decrypt settings.key_check: it is not the key this database was created with")
	}
	return nil
}

// RandomDigits returns n random decimal digits, for example a member password
// (BR-14). It fails if n is outside 1..MaxRandomDigits.
// Bytes of 250 or more are skipped so that every digit is equally likely.
func RandomDigits(n int) (string, error) {
	if n < 1 || n > MaxRandomDigits {
		return "", fmt.Errorf("secret: digit count %d out of range 1..%d", n, MaxRandomDigits)
	}
	out := make([]byte, 0, n)
	buf := make([]byte, n+n/4+8)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 250 && len(out) < n {
				out = append(out, '0'+b%10)
			}
		}
	}
	return string(out), nil
}

// MaxRandomDigits is the longest password RandomDigits produces (settings allow 20).
const MaxRandomDigits = 64
