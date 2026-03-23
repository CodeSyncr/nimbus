package auth

import (
	"context"
	"errors"
	"fmt"
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

type JWTDriver struct {
	secret []byte
}

func NewJWTDriver(secret string) *JWTDriver {
	return &JWTDriver{secret: []byte(secret)}
}

func (d *JWTDriver) Generate(claims map[string]any, expiresAt time.Time) (string, error) {
	jwtClaims := jwt.MapClaims{}
	for k, v := range claims {
		jwtClaims[k] = v
	}
	jwtClaims["exp"] = expiresAt.Unix()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	return token.SignedString(d.secret)
}

func (d *JWTDriver) Parse(tokenStr string) (map[string]any, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return d.secret, nil
	})

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
	key paseto.V4SymmetricKey
}

func NewPasetoDriver(keyStr string) *PasetoDriver {
	key, err := paseto.V4SymmetricKeyFromHex(keyStr)
	if err != nil {
		// If not hex, try from bytes (requires 32 bytes)
		key, _ = paseto.V4SymmetricKeyFromBytes([]byte(keyStr))
	}
	return &PasetoDriver{key: key}
}

func (d *PasetoDriver) Generate(claims map[string]any, expiresAt time.Time) (string, error) {
	token := paseto.NewToken()
	for k, v := range claims {
		token.Set(k, v)
	}
	token.SetExpiration(expiresAt)

	return token.V4Encrypt(d.key, nil), nil
}

func (d *PasetoDriver) Parse(tokenStr string) (map[string]any, error) {
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
		"iat": time.Now().Format(time.RFC3339),
	}
	expiresAt := time.Now().Add(expiresIn)
	return g.driver.Generate(claims, expiresAt)
}
