package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key generation: %v", err)
	}
	c, err := New(key, 0x01)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestCipher_RoundTrip(t *testing.T) {
	c := newTestCipher(t)
	cases := []struct {
		name      string
		plaintext []byte
	}{
		{"short token", []byte("ghp_abc123")},
		{"long token", bytes.Repeat([]byte("x"), 512)},
		{"unicode", []byte("token-wíth-ünïcödé")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := c.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			got, err := c.Decrypt(blob)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if !bytes.Equal(got, tc.plaintext) {
				t.Errorf("round-trip mismatch: got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestCipher_TamperedCiphertext(t *testing.T) {
	c := newTestCipher(t)
	blob, _ := c.Encrypt([]byte("secret"))
	blob[len(blob)-1] ^= 0xFF // flip last byte (GCM tag)
	if _, err := c.Decrypt(blob); err == nil {
		t.Fatal("expected error for tampered ciphertext, got nil")
	}
}

func TestCipher_WrongKey(t *testing.T) {
	c1 := newTestCipher(t)
	c2 := newTestCipher(t)

	blob, _ := c1.Encrypt([]byte("secret"))
	if _, err := c2.Decrypt(blob); err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
}

func TestCipher_EmptyInput(t *testing.T) {
	c := newTestCipher(t)

	blob, err := c.Encrypt(nil)
	if err != nil || blob != nil {
		t.Fatalf("Encrypt(nil): want (nil, nil), got (%v, %v)", blob, err)
	}

	plain, err := c.Decrypt(nil)
	if err != nil || plain != nil {
		t.Fatalf("Decrypt(nil): want (nil, nil), got (%v, %v)", plain, err)
	}

	blob2, err := c.Encrypt([]byte{})
	if err != nil || blob2 != nil {
		t.Fatalf("Encrypt([]): want (nil, nil), got (%v, %v)", blob2, err)
	}
}

func TestCipher_InvalidKeyLength(t *testing.T) {
	if _, err := New([]byte("tooshort"), 0x01); err == nil {
		t.Fatal("expected error for short key, got nil")
	}
}

func TestCipher_UnknownVersion(t *testing.T) {
	c := newTestCipher(t)
	blob, _ := c.Encrypt([]byte("secret"))
	blob[0] = 0xFF // corrupt version byte
	if _, err := c.Decrypt(blob); err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
}

func TestCipher_TooShortBlob(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error for too-short blob, got nil")
	}
}
