package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

func TestRejectUnsafeTokenSecrets(t *testing.T) {
	for _, secret := range []string{"", "short", "please-change-this-secret-to-32-characters"} {
		if ValidateTokenSecret(secret) == nil {
			t.Fatalf("accepted unsafe secret")
		}
		for _, d := range []TokenDriver{NewJWTDriver(secret), NewPasetoDriver(secret)} {
			if _, err := d.Generate(map[string]any{"sub": "u"}, time.Now().Add(time.Hour)); err == nil {
				t.Fatal("issued token with unsafe secret")
			}
			if _, err := d.Parse("invalid"); err == nil {
				t.Fatal("parsed token with unsafe secret")
			}
		}
	}
}

func TestJWTValidationContract(t *testing.T) {
	const secret = "01234567890123456789012345678901"
	d := NewJWTDriver(secret, JWTOptions{Issuer: "issuer", Audience: "api"})
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		claims jwt.MapClaims
		valid  bool
	}{
		{"valid", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "exp": time.Now().Add(time.Hour).Unix(), "iss": "issuer", "aud": "api"}, true},
		{"no expiry", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "iss": "issuer", "aud": "api"}, false},
		{"expired", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "exp": time.Now().Add(-time.Hour).Unix(), "iss": "issuer", "aud": "api"}, false},
		{"wrong algorithm", jwt.SigningMethodHS384, jwt.MapClaims{"sub": "u", "exp": time.Now().Add(time.Hour).Unix(), "iss": "issuer", "aud": "api"}, false},
		{"wrong audience", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "exp": time.Now().Add(time.Hour).Unix(), "iss": "issuer", "aud": "other"}, false},
		{"missing issuer", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "exp": time.Now().Add(time.Hour).Unix(), "aud": "api"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Parse(token)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	token, err := d.Generate(map[string]any{"sub": "u"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Parse(token); err != nil {
		t.Fatal(err)
	}
}
