// Package secretbox encrypts small secrets (OAuth client secrets, refresh
// tokens) before they are written to the database, using AES-256-GCM.
//
// The key comes from the environment (SETTINGS_ENCRYPTION_KEY), never from
// the database, so a leaked database dump or backup alone does not expose the
// secrets. Each value is bound to a caller-supplied "purpose" string as GCM
// additional data, so a ciphertext copied from one column into another (for
// example a client secret pasted over a refresh token) fails to decrypt
// instead of being accepted.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	keySize = 32
	prefix  = "v1:"
)

// ErrDecrypt is returned for anything that fails authentication or parsing,
// deliberately without saying which part: wrong key, wrong purpose, tampered
// or truncated data all look the same.
var ErrDecrypt = errors.New("secretbox: cannot decrypt value")

type Box struct{ aead cipher.AEAD }

// New builds a Box from a raw 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secretbox: key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// ParseKey reads a key as configured in the environment: 64 hex characters,
// or standard/URL-safe base64 (padded or not), decoding to exactly 32 bytes.
// `openssl rand -base64 32` produces a valid one.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("secretbox: key is empty")
	}
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != keySize {
				return nil, fmt.Errorf("secretbox: key must decode to %d bytes, got %d", keySize, len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("secretbox: key must be 64 hex characters or base64 of 32 bytes")
}

// NewFromString is ParseKey + New.
func NewFromString(s string) (*Box, error) {
	key, err := ParseKey(s)
	if err != nil {
		return nil, err
	}
	return New(key)
}

// Encrypt returns "v1:" + base64(nonce || ciphertext). A fresh random nonce
// per call means encrypting the same text twice gives different output.
func (b *Box) Encrypt(plaintext, purpose string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), []byte(purpose))
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (b *Box) Decrypt(encoded, purpose string) (string, error) {
	if !strings.HasPrefix(encoded, prefix) {
		return "", ErrDecrypt
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	nonce, ct := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ct, []byte(purpose))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
