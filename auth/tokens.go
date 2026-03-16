package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// TokenStore manages token creation, hashing, and verification.
// Tokens are stored hashed; only the plaintext is returned once at creation.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*tokenRecord // keyed by user ID
	secret []byte
	ttl    time.Duration
}

type tokenRecord struct {
	hash      string
	expiresAt time.Time
}

// NewTokenStore creates a token store with the given HMAC secret and token TTL.
func NewTokenStore(secret string, ttl time.Duration) *TokenStore {
	return &TokenStore{
		tokens: make(map[string]*tokenRecord),
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Create generates a new token for the given user ID.
// Returns the plaintext token. The store keeps only the hash.
func (s *TokenStore) Create(userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth/tokens: %w", err)
	}
	plaintext := hex.EncodeToString(raw)
	hash := s.hashToken(plaintext)

	s.mu.Lock()
	s.tokens[userID] = &tokenRecord{
		hash:      hash,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()

	return plaintext, nil
}

// Verify checks the plaintext token against the stored hash for the user.
// Returns true and deletes the token on success.
func (s *TokenStore) Verify(userID, plaintext string) bool {
	s.mu.RLock()
	rec, ok := s.tokens[userID]
	s.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(rec.expiresAt) {
		s.Delete(userID)
		return false
	}
	hash := s.hashToken(plaintext)
	if !hmac.Equal([]byte(hash), []byte(rec.hash)) {
		return false
	}
	s.Delete(userID)
	return true
}

// Delete removes a stored token for the user.
func (s *TokenStore) Delete(userID string) {
	s.mu.Lock()
	delete(s.tokens, userID)
	s.mu.Unlock()
}

// Exists checks if a non-expired token exists for the user.
func (s *TokenStore) Exists(userID string) bool {
	s.mu.RLock()
	rec, ok := s.tokens[userID]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(rec.expiresAt)
}

func (s *TokenStore) hashToken(plaintext string) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(plaintext))
	return hex.EncodeToString(h.Sum(nil))
}

// Cleanup removes all expired tokens. Call periodically (e.g. via scheduler).
func (s *TokenStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, rec := range s.tokens {
		if now.After(rec.expiresAt) {
			delete(s.tokens, id)
		}
	}
}
