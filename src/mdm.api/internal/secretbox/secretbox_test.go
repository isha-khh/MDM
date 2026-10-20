package secretbox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testBox(t *testing.T, fill byte) *Box {
	t.Helper()
	b, err := New(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	b := testBox(t, 1)
	for _, plain := range []string{"GOCSPX-abc123", "", "中文密碼 with spaces\nand newline", strings.Repeat("x", 5000)} {
		enc, err := b.Encrypt(plain, "client_secret")
		if err != nil {
			t.Fatal(err)
		}
		got, err := b.Decrypt(enc, "client_secret")
		if err != nil || got != plain {
			t.Errorf("round trip of %q: got %q, err %v", plain, got, err)
		}
	}
}

func TestCiphertextDoesNotContainPlaintextAndIsRandomised(t *testing.T) {
	b := testBox(t, 1)
	enc1, _ := b.Encrypt("super-secret-value", "p")
	enc2, _ := b.Encrypt("super-secret-value", "p")
	if strings.Contains(enc1, "super-secret-value") {
		t.Error("ciphertext leaks plaintext")
	}
	if !strings.HasPrefix(enc1, "v1:") {
		t.Errorf("missing version prefix: %q", enc1)
	}
	if enc1 == enc2 {
		t.Error("same plaintext must encrypt differently each time (random nonce)")
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	enc, _ := testBox(t, 1).Encrypt("secret", "p")
	if _, err := testBox(t, 2).Decrypt(enc, "p"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong key: got %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsWrongPurpose(t *testing.T) {
	// A client secret ciphertext must not be accepted in the refresh token slot.
	b := testBox(t, 1)
	enc, _ := b.Encrypt("secret", "google_calendar.client_secret")
	if _, err := b.Decrypt(enc, "google_calendar.refresh_token"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong purpose: got %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsTamperedAndMalformed(t *testing.T) {
	b := testBox(t, 1)
	enc, _ := b.Encrypt("secret", "p")

	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, "v1:"))
	raw[len(raw)-1] ^= 0xff // flip a bit in the auth tag
	tampered := "v1:" + base64.StdEncoding.EncodeToString(raw)

	cases := map[string]string{
		"tampered":        tampered,
		"no prefix":       strings.TrimPrefix(enc, "v1:"),
		"unknown version": "v2:" + strings.TrimPrefix(enc, "v1:"),
		"not base64":      "v1:%%%%",
		"too short":       "v1:" + base64.StdEncoding.EncodeToString([]byte("abc")),
		"empty":           "",
		"plaintext":       "just a plain old password",
	}
	for name, in := range cases {
		if _, err := b.Decrypt(in, "p"); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: got %v, want ErrDecrypt", name, err)
		}
	}
}

func TestNewRejectsBadKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("%d-byte key should be rejected", n)
		}
	}
}

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xab}, 32)
	good := map[string]string{
		"hex":            hex.EncodeToString(raw),
		"hex upper":      strings.ToUpper(hex.EncodeToString(raw)),
		"base64":         base64.StdEncoding.EncodeToString(raw),
		"base64 no pad":  base64.RawStdEncoding.EncodeToString(raw),
		"base64 url":     base64.URLEncoding.EncodeToString(raw),
		"surrounding ws": "  " + base64.StdEncoding.EncodeToString(raw) + "\n",
	}
	for name, in := range good {
		got, err := ParseKey(in)
		if err != nil || !bytes.Equal(got, raw) {
			t.Errorf("%s: got %x, err %v", name, got, err)
		}
	}

	bad := map[string]string{
		"empty":            "",
		"blank":            "   ",
		"16 bytes base64":  base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"48 bytes base64":  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 48)),
		"not a key":        "change-me-in-production",
		"short hex":        hex.EncodeToString(make([]byte, 16)),
		"64 chars not hex": strings.Repeat("z", 64),
	}
	for name, in := range bad {
		if _, err := ParseKey(in); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestNewFromString(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	a, err := NewFromString(key)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := a.Encrypt("hello", "p")
	b, _ := NewFromString(key) // separate instance, same key: e.g. after a restart
	if got, err := b.Decrypt(enc, "p"); err != nil || got != "hello" {
		t.Errorf("same key across instances: got %q, %v", got, err)
	}
}
