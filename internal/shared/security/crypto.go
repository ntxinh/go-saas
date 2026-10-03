// Package security provides AES-256-GCM encryption for PII columns.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Cipher encrypts PII strings to bytea blobs (nonce prepended).
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 64-char hex key (32-byte AES-256-GCM key).
func New(hexKey string) (*Cipher, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("security: decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("security: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("security: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("security: gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext with AES-256-GCM (random nonce prepended).
func (c *Cipher) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("security: nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

// Decrypt opens a nonce||ciphertext blob produced by Encrypt.
func (c *Cipher) Decrypt(blob []byte) (string, error) {
	ns := c.aead.NonceSize()
	if len(blob) < ns+c.aead.Overhead() {
		return "", fmt.Errorf("security: blob too short (%d bytes)", len(blob))
	}
	plain, err := c.aead.Open(nil, blob[:ns], blob[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("security: decrypt: %w", err)
	}
	return string(plain), nil
}

// MayDecrypt decrypts nullable PII columns: a nil blob maps to "". It is
// nil-receiver safe so services may run without PII_KEY configured; real
// ciphertext is never silently dropped — decrypt failures still error.
func (c *Cipher) MayDecrypt(blob []byte) (string, error) {
	if c == nil || len(blob) == 0 {
		return "", nil
	}
	return c.Decrypt(blob)
}
