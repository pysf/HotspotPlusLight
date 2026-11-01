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

// Box encrypts and decrypts with one key.
type Box struct{ gcm cipher.AEAD }

// ParseKey decodes the base64 32-byte HSP_SECRET_KEY.
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

// Encrypt returns nonce || ciphertext.
func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.gcm.Seal(nonce, nonce, plain, nil), nil
}

func (b *Box) Decrypt(data []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(data) < n {
		return nil, errors.New("ciphertext too short")
	}
	return b.gcm.Open(nil, data[:n], data[n:], nil)
}

// KeyCheck produces the value stored in settings.key_check.
func (b *Box) KeyCheck() (string, error) {
	enc, err := b.Encrypt([]byte(KeyCheckPlaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// VerifyKeyCheck reports whether stored was produced with this box's key.
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

// RandomDigits returns n random decimal digits (crypto/rand), e.g. a 6-digit member password (BR-14).
func RandomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = '0' + b[i]%10 // 256 % 10 != 0: a negligible bias that does not matter for a short voucher PIN
	}
	return string(b), nil
}
