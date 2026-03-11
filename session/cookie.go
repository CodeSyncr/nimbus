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
)

// NewCookieStore creates a cookie-based session store.
// Session data is encrypted in the cookie. No server-side storage; suitable for small payloads (e.g. user_id).
// Key must be 32 bytes for AES-256; use KeyFromString(APP_KEY) to derive from a string.
func NewCookieStore(key []byte) *CookieStoreImpl {
	if len(key) != 32 {
		h := sha256.Sum256(key)
		key = h[:]
	}
	return &CookieStoreImpl{key: key}
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

// KeyFromString derives a 32-byte key from a string (e.g. APP_KEY).
func KeyFromString(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
