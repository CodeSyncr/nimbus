package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/CodeSyncr/nimbus/redis"
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
	redis  *redis.Client
	prefix string
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
	if s.ttl <= 0 {
		return "", fmt.Errorf("auth/tokens: TTL must be positive")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth/tokens: %w", err)
	}
	plaintext := hex.EncodeToString(raw)
	hash := s.hashToken(plaintext)

	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.redis.Set(ctx, s.prefix+userID, hash, s.ttl).Err(); err != nil {
			return "", err
		}
		return plaintext, nil
	}
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
	ok, _ := s.VerifyWithError(userID, plaintext)
	return ok
}

// VerifyWithError atomically consumes a matching live token and reports store failures.
func (s *TokenStore) VerifyWithError(userID, plaintext string) (bool, error) {
	hash := s.hashToken(plaintext)
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, err := consumeToken.Run(ctx, s.redis, []string{s.prefix + userID}, hash).Int()
		return n == 1 && err == nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tokens[userID]
	if !ok {
		return false, nil
	}
	if !time.Now().Before(rec.expiresAt) {
		delete(s.tokens, userID)
		return false, nil
	}
	if !hmac.Equal([]byte(hash), []byte(rec.hash)) {
		return false, nil
	}
	delete(s.tokens, userID)
	return true, nil
}

// NewRedisTokenStore shares single-use tokens across instances. Use a distinct
// nonempty prefix per purpose (password reset vs email verification).
func NewRedisTokenStore(client *redis.Client, prefix, secret string, ttl time.Duration) *TokenStore {
	if client == nil || prefix == "" {
		panic("auth/tokens: Redis client and purpose prefix required")
	}
	s := NewTokenStore(secret, ttl)
	s.redis, s.prefix = client, prefix
	return s
}

var consumeToken = redis.NewScript(`
local stored = redis.call('GET', KEYS[1])
if stored and stored == ARGV[1] then
 redis.call('DEL', KEYS[1])
 return 1
end
return 0
`)

// Delete removes a stored token for the user.
func (s *TokenStore) Delete(userID string) {
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.redis.Del(ctx, s.prefix+userID)
		return
	}
	s.mu.Lock()
	delete(s.tokens, userID)
	s.mu.Unlock()
}

// Exists checks if a non-expired token exists for the user.
func (s *TokenStore) Exists(userID string) bool {
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, err := s.redis.Exists(ctx, s.prefix+userID).Result()
		return err == nil && n > 0
	}
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
	if s.redis != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, rec := range s.tokens {
		if now.After(rec.expiresAt) {
			delete(s.tokens, id)
		}
	}
}
