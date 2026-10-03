package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/golang-jwt/jwt/v5"
)

// TokenDriver defines the interface for stateless token strategies.
type TokenDriver interface {
	Generate(claims map[string]any, expiresAt time.Time) (string, error)
	Parse(token string) (map[string]any, error)
}

// ── JWT Driver ───────────────────────────────────────────────────

// JWTOptions scopes tokens to an issuer and audience when configured.
type JWTOptions struct {
	Issuer   string
	Audience string
}

type JWTDriver struct {
	options   JWTOptions
	secret    []byte
	configErr error
}

func NewJWTDriver(secret string, options ...JWTOptions) *JWTDriver {
	d := &JWTDriver{secret: []byte(secret), configErr: ValidateTokenSecret(secret)}
	if len(options) > 0 {
		d.options = options[0]
	}
	return d
}

func (d *JWTDriver) Generate(claims map[string]any, expiresAt time.Time) (string, error) {
	if d.configErr != nil {
		return "", d.configErr
	}
	jwtClaims := jwt.MapClaims{}
	for k, v := range claims {
		jwtClaims[k] = v
	}
	jwtClaims["exp"] = expiresAt.Unix()
	if d.options.Issuer != "" {
		jwtClaims["iss"] = d.options.Issuer
	}
	if d.options.Audience != "" {
		jwtClaims["aud"] = d.options.Audience
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	return token.SignedString(d.secret)
}

func (d *JWTDriver) Parse(tokenStr string) (map[string]any, error) {
	if d.configErr != nil {
		return nil, d.configErr
	}
	options := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired()}
	if d.options.Issuer != "" {
		options = append(options, jwt.WithIssuer(d.options.Issuer))
	}
	if d.options.Audience != "" {
		options = append(options, jwt.WithAudience(d.options.Audience))
	}
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return d.secret, nil
	}, options...)

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// ── PASETO Driver ────────────────────────────────────────────────

type PasetoDriver struct {
	key       paseto.V4SymmetricKey
	configErr error
}

// NewPasetoDriver builds a PASETO v4.local driver from keyStr. For strong
// security keyStr should be 32 random bytes encoded as 64 hex characters.
//
// If keyStr is not valid hex, a raw 32-byte key is accepted as-is; any other
// input is hashed with SHA-256 to a deterministic 32-byte key. This guarantees
// a valid, input-derived key is always used — the previous implementation
// silently discarded the error and could fall back to a zero/garbage key,
// weakening every token without any signal.
func NewPasetoDriver(keyStr string) *PasetoDriver {
	if err := ValidateTokenSecret(keyStr); err != nil {
		return &PasetoDriver{configErr: err}
	}
	key, err := paseto.V4SymmetricKeyFromHex(keyStr)
	if err != nil {
		raw := []byte(keyStr)
		if len(raw) != 32 {
			sum := sha256.Sum256(raw)
			raw = sum[:]
		}
		key, err = paseto.V4SymmetricKeyFromBytes(raw)
		if err != nil {
			// Unreachable: raw is always exactly 32 bytes here.
			panic(fmt.Sprintf("auth: invalid PASETO key: %v", err))
		}
	}
	return &PasetoDriver{key: key}
}

func (d *PasetoDriver) Generate(claims map[string]any, expiresAt time.Time) (string, error) {
	if d.configErr != nil {
		return "", d.configErr
	}
	token := paseto.NewToken()
	for k, v := range claims {
		token.Set(k, v)
	}
	token.SetExpiration(expiresAt)

	return token.V4Encrypt(d.key, nil), nil
}

func (d *PasetoDriver) Parse(tokenStr string) (map[string]any, error) {
	if d.configErr != nil {
		return nil, d.configErr
	}
	parser := paseto.NewParser()
	token, err := parser.ParseV4Local(d.key, tokenStr, nil)
	if err != nil {
		return nil, err
	}

	claims := make(map[string]any)
	for k, v := range token.Claims() {
		claims[k] = v
	}

	return claims, nil
}

// ── Stateless Guard ──────────────────────────────────────────────

type StatelessGuard struct {
	driver TokenDriver
	loader UserLoader
}

func NewStatelessGuard(driver TokenDriver, loader UserLoader) *StatelessGuard {
	return &StatelessGuard{
		driver: driver,
		loader: loader,
	}
}

func (g *StatelessGuard) User(ctx context.Context) (User, error) {
	token := tokenFromContext(ctx)
	if token == "" {
		return nil, nil
	}

	claims, err := g.driver.Parse(token)
	if err != nil {
		return nil, nil
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return nil, nil
	}

	return g.loader.LoadUser(ctx, sub)
}

func (g *StatelessGuard) Login(_ context.Context, _ User) error {
	return nil
}

func (g *StatelessGuard) Logout(_ context.Context) error {
	return nil
}

// GenerateToken creates a new token for the user using the configured driver.
func (g *StatelessGuard) GenerateToken(userID string, expiresIn time.Duration) (string, error) {
	claims := map[string]any{
		"sub": userID,
		"iat": time.Now().Unix(),
	}
	expiresAt := time.Now().Add(expiresIn)
	return g.driver.Generate(claims, expiresAt)
}

// ValidateTokenSecret rejects missing, short, and known scaffold secrets.
// Use a cryptographically random secret; length alone does not prove entropy.
func ValidateTokenSecret(secret string) error {
	if len(strings.TrimSpace(secret)) < 32 || strings.Contains(strings.ToLower(secret), "please-change-this-secret") {
		return errors.New("auth: token secret must contain at least 32 random characters and must not be a scaffold placeholder")
	}
	return nil
}
