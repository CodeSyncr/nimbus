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

// NewCookieStore creates encrypted sessions with a process-local validity registry.
// Sessions are invalidated on restart. For multiple instances use
// NewCookieStoreWithRegistry with a shared Redis or database Store.
func NewCookieStore(key []byte) *CookieStoreImpl {
	return NewCookieStoreWithRegistry(key, NewMemoryStore())
}

// NewCookieStoreWithRegistry stores only cookie fingerprints server-side,
// enabling expiry, replacement and logout revocation across instances.
// registry must be a server-side Store, not another cookie store.
func NewCookieStoreWithRegistry(key []byte, registry Store) *CookieStoreImpl {
	if registry == nil {
		panic("session: cookie registry is required")
	}
	if _, ok := registry.(*CookieStoreImpl); ok {
		panic("session: cookie registry must be server-side")
	}
	if len(key) != 32 {
		key = deriveKey(key)
	}
	return &CookieStoreImpl{key: key, registry: registry}
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
	key      []byte
	registry Store
}

type cookiePayload struct {
	Version   int            `json:"v"`
	ExpiresAt int64          `json:"expires_at"`
	Data      map[string]any `json:"data"`
}

func cookieFingerprint(id string) string {
	h := sha256.Sum256([]byte(id))
	return "cookie:" + base64.RawURLEncoding.EncodeToString(h[:])
}

func (s *CookieStoreImpl) Get(ctx context.Context, id string) (map[string]any, error) {
	if id == "" {
		return nil, nil
	}
	dec, err := s.decrypt(id)
	if err != nil {
		return nil, nil
	}
	var payload cookiePayload
	if json.Unmarshal(dec, &payload) != nil || payload.Version != 1 ||
		payload.ExpiresAt <= time.Now().UnixNano() {
		return nil, nil
	}
	active, err := s.registry.Get(ctx, cookieFingerprint(id))
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, nil
	}
	return payload.Data, nil
}

func (s *CookieStoreImpl) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if maxAge <= 0 {
		return "", fmt.Errorf("session: cookie lifetime must be positive")
	}
	enc, err := s.encrypt(cookiePayload{Version: 1, ExpiresAt: time.Now().Add(maxAge).UnixNano(), Data: data})
	if err != nil {
		return "", err
	}
	fingerprint := cookieFingerprint(enc)
	if _, err := s.registry.Set(ctx, fingerprint, map[string]any{"active": true}, maxAge); err != nil {
		return "", err
	}
	if id != "" {
		if err := s.Destroy(ctx, id); err != nil {
			_ = s.registry.Destroy(ctx, fingerprint)
			return "", err
		}
	}
	return enc, nil
}

func (s *CookieStoreImpl) Destroy(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return s.registry.Destroy(ctx, cookieFingerprint(id))
}

func (s *CookieStoreImpl) encrypt(data any) (string, error) {
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
