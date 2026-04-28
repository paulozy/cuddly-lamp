package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	nonceSize  = 12
	headerSize = 1 + nonceSize // version byte + nonce
)

var errInvalidCiphertext = errors.New("crypto: invalid ciphertext")

// Cipher encrypts and decrypts values using AES-256-GCM.
// Ciphertext format: [version:1][nonce:12][ciphertext+tag].
type Cipher struct {
	gcm        cipher.AEAD
	keyVersion byte
}

// New creates a Cipher from a 32-byte key. version is embedded in every
// ciphertext so future key rotation can be detected without a schema change.
func New(key []byte, version byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	return &Cipher{gcm: gcm, keyVersion: version}, nil
}

// Encrypt encrypts plaintext and returns [version||nonce||ciphertext+tag].
// An empty or nil input returns nil, nil (NULL column support).
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce generation failed: %w", err)
	}

	sealed := c.gcm.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 1+nonceSize+len(sealed))
	out[0] = c.keyVersion
	copy(out[1:], nonce)
	copy(out[1+nonceSize:], sealed)
	return out, nil
}

// Decrypt decrypts a blob produced by Encrypt.
// A nil or empty input returns nil, nil.
func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	if len(blob) < headerSize {
		return nil, errInvalidCiphertext
	}
	if blob[0] != c.keyVersion {
		return nil, fmt.Errorf("crypto: unknown key version %d", blob[0])
	}

	nonce := blob[1 : 1+nonceSize]
	ciphertext := blob[1+nonceSize:]

	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decryption failed: %w", err)
	}
	return plaintext, nil
}
