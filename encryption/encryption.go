package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ── Errors ──────────────────────────────────────────────────────

var (
	ErrInvalidKey        = errors.New("encryption: key must be 16, 24, or 32 bytes (AES-128/192/256)")
	ErrDecryptFailed     = errors.New("encryption: decryption failed — ciphertext tampered or wrong key")
	ErrInvalidCiphertext = errors.New("encryption: ciphertext too short")
)

// ── Encrypter ───────────────────────────────────────────────────

// Encrypter provides AES-256-GCM authenticated encryption.
type Encrypter struct {
	key []byte
}

// New creates an Encrypter. key must be 16/24/32 bytes.
// You can pass a hex-encoded or base64-encoded key — it will be decoded automatically.
func New(key string) (*Encrypter, error) {
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	switch len(raw) {
	case 16, 24, 32:
	default:
		return nil, ErrInvalidKey
	}
	return &Encrypter{key: raw}, nil
}

// MustNew is like New but panics on error.
func MustNew(key string) *Encrypter {
	e, err := New(key)
	if err != nil {
		panic(err)
	}
	return e
}

// ── Encrypt / Decrypt (bytes) ───────────────────────────────────

// Encrypt encrypts plaintext and returns nonce+ciphertext (raw bytes).
func (e *Encrypter) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}

	// nonce is prepended to the ciphertext
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts data produced by Encrypt.
func (e *Encrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// ── Encrypt / Decrypt (strings, base64-encoded) ─────────────────

// EncryptString encrypts a string and returns a base64-encoded ciphertext.
func (e *Encrypter) EncryptString(plaintext string) (string, error) {
	ct, err := e.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptString decrypts a base64-encoded ciphertext produced by EncryptString.
func (e *Encrypter) DecryptString(encoded string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("encryption: invalid base64: %w", err)
	}
	pt, err := e.Decrypt(ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ── Key Generation ──────────────────────────────────────────────

// GenerateKey generates a random key of the given byte length (16/24/32)
// and returns it hex-encoded.
func GenerateKey(size int) (string, error) {
	switch size {
	case 16, 24, 32:
	default:
		return "", fmt.Errorf("encryption: key size must be 16, 24, or 32 bytes")
	}
	key := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("encryption: %w", err)
	}
	return hex.EncodeToString(key), nil
}

// GenerateKey256 generates a random 32-byte AES-256 key (hex-encoded).
func GenerateKey256() (string, error) {
	return GenerateKey(32)
}

// ── Helpers ─────────────────────────────────────────────────────

// decodeKey tries hex then base64, otherwise returns raw bytes.
func decodeKey(key string) ([]byte, error) {
	// Try hex
	if b, err := hex.DecodeString(key); err == nil && len(key)%2 == 0 {
		return b, nil
	}

	// Try base64
	if strings.HasSuffix(key, "=") || len(key)%4 == 0 {
		if b, err := base64.StdEncoding.DecodeString(key); err == nil {
			return b, nil
		}
	}

	// Raw bytes
	return []byte(key), nil
}

// ── Hash helpers (deterministic, for comparison — NOT for passwords) ──

// EncryptDeterministic encrypts with a zero nonce (same input → same output).
// DO NOT use for general-purpose encryption; only for searchable columns.
func (e *Encrypter) EncryptDeterministic(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize()) // zero nonce
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}
