package session

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/hkdf"
)

// NewCookieStore creates a cookie-based session store.
// Session data is encrypted in the cookie. No server-side storage; suitable for small payloads (e.g. user_id).
// Key should be 32 random bytes for AES-256; any other length is run through
// HKDF-SHA256 to derive a 32-byte key. Use KeyFromString(APP_KEY) for strings.
func NewCookieStore(key []byte) *CookieStoreImpl {
	if len(key) != 32 {
		key = deriveKey(key)
	}
	return &CookieStoreImpl{key: key}
}

// deriveKey derives a 32-byte AES-256 key from secret using HKDF-SHA256 with
// domain separation, replacing a bare unsalted SHA-256. HKDF assumes a
// high-entropy secret (APP_KEY should be 32 random bytes); it is a key
// derivation function, not a password-stretching KDF — do not rely on it to
// protect a low-entropy passphrase.
func deriveKey(secret []byte) []byte {
	r := hkdf.New(sha256.New, secret, []byte("nimbus/session/cookie"), []byte("aes-256-gcm key v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		// Unreachable for 32 bytes of HKDF-SHA256 output; derive defensively.
		h := sha256.Sum256(secret)
		copy(key, h[:])
	}
	return key
}

type CookieStoreImpl struct {
	key []byte
}

func (s *CookieStoreImpl) Get(ctx context.Context, id string) (map[string]any, error) {
	// id is the cookie value
	if id == "" {
		return nil, nil
	}
	dec, err := s.decrypt(id)
	if err != nil {
		return nil, nil
	}
	var data map[string]any
	if err := json.Unmarshal(dec, &data); err != nil {
		return nil, nil
	}
	return data, nil
}

func (s *CookieStoreImpl) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	enc, err := s.encrypt(data)
	if err != nil {
		return "", err
	}
	return enc, nil
}

func (s *CookieStoreImpl) Destroy(ctx context.Context, id string) error {
	return nil
}

func (s *CookieStoreImpl) encrypt(data map[string]any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, raw, nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func (s *CookieStoreImpl) decrypt(encoded string) ([]byte, error) {
	ciphertext, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// KeyFromString derives a 32-byte key from a string (e.g. APP_KEY) using
// HKDF-SHA256. Provide a high-entropy APP_KEY (32 random bytes); this is not a
// password-stretching KDF.
func KeyFromString(s string) []byte {
	return deriveKey([]byte(s))
}
