package supabase

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
)

// createTestJWT helper generates a signed HS256 JWT token.
func createTestJWT(sub, email string, exp int64, secret string) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"sub":"%s","email":"%s","exp":%d,"role":"authenticated","aud":"authenticated","iss":"http://localhost:8080/auth/v1"}`, sub, email, exp)

	hB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	pB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))

	signingInput := hB64 + "." + pB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := mac.Sum(nil)
	sB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + sB64
}

func TestVerifySupabaseJWT(t *testing.T) {
	secret := "super-secret-key-1234567890123456"
	userID := "user-123-abc"
	email := "test@example.com"

	t.Run("valid token", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT(userID, email, exp, secret)

		claims, err := verifySupabaseJWT(token, secret, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if claims.Sub != userID || claims.Email != email {
			t.Errorf("claims mismatch: sub=%s, email=%s", claims.Sub, claims.Email)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		exp := time.Now().Add(-1 * time.Hour).Unix()
		token := createTestJWT(userID, email, exp, secret)

		_, err := verifySupabaseJWT(token, secret, "")
		if err == nil {
			t.Fatal("expected expired token error, got nil")
		}
		if !strings.Contains(err.Error(), "expired") {
			t.Errorf("expected expired error, got: %v", err)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT(userID, email, exp, "wrong-secret-key")

		_, err := verifySupabaseJWT(token, secret, "")
		if err == nil {
			t.Fatal("expected signature verification failure, got nil")
		}
		if !strings.Contains(err.Error(), "signature verification failed") {
			t.Errorf("expected signature verification failure error, got: %v", err)
		}
	})

	t.Run("malformed token", func(t *testing.T) {
		_, err := verifySupabaseJWT("invalid.token.format", secret, "")
		if err == nil {
			t.Fatal("expected malformed token error, got nil")
		}
	})

	t.Run("alg:none is rejected", func(t *testing.T) {
		// Forge an unsigned token with alg:none — must be rejected even though
		// the payload is otherwise valid.
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		exp := time.Now().Add(1 * time.Hour).Unix()
		payload := base64.RawURLEncoding.EncodeToString([]byte(
			fmt.Sprintf(`{"sub":"%s","email":"%s","exp":%d,"role":"authenticated","aud":"authenticated"}`, userID, email, exp)))
		token := header + "." + payload + "."

		if _, err := verifySupabaseJWT(token, secret, ""); err == nil {
			t.Fatal("expected alg:none token to be rejected, got nil")
		}
	})

	t.Run("service_role token is rejected", func(t *testing.T) {
		// The service_role API key is a JWT signed with the same secret; it must
		// never authorize an end-user request.
		exp := time.Now().Add(1 * time.Hour).Unix()
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(
			fmt.Sprintf(`{"exp":%d,"role":"service_role","aud":"authenticated"}`, exp)))
		signingInput := header + "." + payload
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(signingInput))
		token := signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

		if _, err := verifySupabaseJWT(token, secret, ""); err == nil {
			t.Fatal("expected service_role token to be rejected, got nil")
		}
	})

	t.Run("issuer mismatch is rejected", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT(userID, email, exp, secret) // iss=http://localhost:8080/auth/v1
		if _, err := verifySupabaseJWT(token, secret, "https://evil.example.com/auth/v1"); err == nil {
			t.Fatal("expected issuer mismatch to be rejected, got nil")
		}
	})

	t.Run("empty secret is rejected", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT(userID, email, exp, secret)
		if _, err := verifySupabaseJWT(token, "", ""); err == nil {
			t.Fatal("expected empty-secret verification to fail, got nil")
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	secret := "secret-jwt-key"
	client := NewClient(Config{
		URL:       "http://localhost:8080",
		AnonKey:   "anon",
		JWTSecret: secret,
	})
	SetGlobal(client)

	mw := AuthMiddleware()

	t.Run("missing auth header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		ctx := nhttp.New(rec, req, nil)

		handler := mw(func(c *nhttp.Context) error {
			return nil
		})

		err := handler(ctx)
		if err != nil {
			t.Fatalf("unexpected middleware error: %v", err)
		}

		if rec.Code != 401 {
			t.Errorf("expected status 401, got %d", rec.Code)
		}
	})

	t.Run("invalid format auth header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "SomeToken xyz")
		ctx := nhttp.New(rec, req, nil)

		handler := mw(func(c *nhttp.Context) error {
			return nil
		})

		err := handler(ctx)
		if err != nil {
			t.Fatalf("unexpected middleware error: %v", err)
		}

		if rec.Code != 401 {
			t.Errorf("expected status 401, got %d", rec.Code)
		}
	})

	t.Run("valid auth header sets claims", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT("user-123", "user@example.com", exp, secret)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		ctx := nhttp.New(rec, req, nil)

		called := false
		handler := mw(func(c *nhttp.Context) error {
			called = true
			userID := GetUserID(c)
			if userID != "user-123" {
				t.Errorf("expected userID user-123, got %s", userID)
			}
			claims := GetClaims(c)
			if claims == nil || claims.Email != "user@example.com" {
				t.Errorf("expected claims for user@example.com, got %v", claims)
			}
			return nil
		})

		err := handler(ctx)
		if err != nil {
			t.Fatalf("unexpected middleware error: %v", err)
		}

		if !called {
			t.Fatal("expected next handler to be called")
		}
	})
}

func TestOptionalAuthMiddleware(t *testing.T) {
	secret := "secret-jwt-key"
	client := NewClient(Config{
		URL:       "http://localhost:8080",
		AnonKey:   "anon",
		JWTSecret: secret,
	})
	SetGlobal(client)

	mw := OptionalAuthMiddleware()

	t.Run("missing auth header allowed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		ctx := nhttp.New(rec, req, nil)

		called := false
		handler := mw(func(c *nhttp.Context) error {
			called = true
			userID := GetUserID(c)
			if userID != "" {
				t.Errorf("expected empty userID, got %s", userID)
			}
			return nil
		})

		err := handler(ctx)
		if err != nil {
			t.Fatalf("unexpected middleware error: %v", err)
		}

		if !called {
			t.Fatal("expected handler to be called")
		}
	})

	t.Run("valid auth header sets claims", func(t *testing.T) {
		exp := time.Now().Add(1 * time.Hour).Unix()
		token := createTestJWT("user-456", "user456@example.com", exp, secret)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		ctx := nhttp.New(rec, req, nil)

		called := false
		handler := mw(func(c *nhttp.Context) error {
			called = true
			userID := GetUserID(c)
			if userID != "user-456" {
				t.Errorf("expected userID user-456, got %s", userID)
			}
			return nil
		})

		err := handler(ctx)
		if err != nil {
			t.Fatalf("unexpected middleware error: %v", err)
		}

		if !called {
			t.Fatal("expected handler to be called")
		}
	})
}

func TestAuthClientRequests(t *testing.T) {
	t.Run("UpdateUser sends JSON body and headers", func(t *testing.T) {
		accessToken := "user-access-token"
		expectedEmail := "updated@example.com"
		expectedPassword := "new-password-999"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				t.Errorf("expected method PUT, got %s", r.Method)
			}
			if r.URL.Path != "/auth/v1/user" {
				t.Errorf("expected path /auth/v1/user, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer "+accessToken {
				t.Errorf("expected auth header Bearer %s, got %s", accessToken, r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}

			// Read body
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			var req UpdateUserRequest
			if err := json.Unmarshal(b, &req); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}
			if req.Email != expectedEmail {
				t.Errorf("expected email %s, got %s", expectedEmail, req.Email)
			}
			if req.Password != expectedPassword {
				t.Errorf("expected password %s, got %s", expectedPassword, req.Password)
			}

			// Return mock response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"user-123","email":"updated@example.com"}`))
		}))
		defer server.Close()

		client := NewClient(Config{
			URL:            server.URL,
			AnonKey:        "anon-key",
			ServiceRoleKey: "service-key",
			JWTSecret:      "jwt-secret",
		})

		updateReq := UpdateUserRequest{
			Email:    expectedEmail,
			Password: expectedPassword,
		}
		user, err := client.Auth.UpdateUser(accessToken, updateReq)
		if err != nil {
			t.Fatalf("UpdateUser failed: %v", err)
		}
		if user == nil || user.Email != expectedEmail {
			t.Errorf("expected user email %s, got %v", expectedEmail, user)
		}
	})

	t.Run("InviteByEmail sends service role key", func(t *testing.T) {
		serviceKey := "service-role-super-key"
		expectedEmail := "invitee@example.com"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected method POST, got %s", r.Method)
			}
			if r.URL.Path != "/auth/v1/invite" {
				t.Errorf("expected path /auth/v1/invite, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer "+serviceKey {
				t.Errorf("expected Authorization header Bearer %s, got %s", serviceKey, r.Header.Get("Authorization"))
			}

			var req InviteRequest
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &req)

			if req.Email != expectedEmail {
				t.Errorf("expected email %s, got %s", expectedEmail, req.Email)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"invited-user-id","email":"invitee@example.com"}`))
		}))
		defer server.Close()

		client := NewClient(Config{
			URL:            server.URL,
			AnonKey:        "anon-key",
			ServiceRoleKey: serviceKey,
			JWTSecret:      "jwt-secret",
		})

		user, err := client.Auth.InviteByEmail(InviteRequest{Email: expectedEmail})
		if err != nil {
			t.Fatalf("InviteByEmail failed: %v", err)
		}
		if user == nil || user.Email != expectedEmail {
			t.Errorf("expected invited user email %s, got %v", expectedEmail, user)
		}
	})

	t.Run("GenerateLink sends service role key", func(t *testing.T) {
		serviceKey := "service-role-super-key"
		expectedEmail := "link@example.com"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected method POST, got %s", r.Method)
			}
			if r.URL.Path != "/auth/v1/admin/generate_link" {
				t.Errorf("expected path /auth/v1/admin/generate_link, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer "+serviceKey {
				t.Errorf("expected Authorization header Bearer %s, got %s", serviceKey, r.Header.Get("Authorization"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"action_link":"https://supabase.co/link-xyz"}`))
		}))
		defer server.Close()

		client := NewClient(Config{
			URL:            server.URL,
			AnonKey:        "anon-key",
			ServiceRoleKey: serviceKey,
			JWTSecret:      "jwt-secret",
		})

		res, err := client.Auth.GenerateLink("signup", expectedEmail, nil)
		if err != nil {
			t.Fatalf("GenerateLink failed: %v", err)
		}
		if res == nil || res["action_link"] != "https://supabase.co/link-xyz" {
			t.Errorf("expected action_link key in response, got %v", res)
		}
	})
}

func TestStorageClientPaths(t *testing.T) {
	t.Run("path encoding on storage upload, update, download, publicURL, signedURL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check path segments are properly encoded in the URL path.
			// Path expected: /storage/v1/object/my-bucket/folder%20name/my%20file.png
			expectedPath := "/storage/v1/object/my-bucket/folder%20name/my%20file.png"
			expectedSignPath := "/storage/v1/object/sign/my-bucket/folder%20name/my%20file.png"

			if r.URL.EscapedPath() != expectedPath && r.URL.EscapedPath() != expectedSignPath {
				t.Errorf("expected path %s or %s, got %s", expectedPath, expectedSignPath, r.URL.EscapedPath())
			}

			w.WriteHeader(200)
			w.Write([]byte(`{"message":"ok"}`))
		}))
		defer server.Close()

		client := NewClient(Config{
			URL:            server.URL,
			AnonKey:        "anon",
			ServiceRoleKey: "service",
		})

		bucket := client.Storage.From("my-bucket")
		testPath := "folder name/my file.png"

		// 1. Upload
		err := bucket.Upload(testPath, strings.NewReader("hello"), "image/png")
		if err != nil {
			t.Fatalf("Upload failed: %v", err)
		}

		// 2. Update
		err = bucket.Update(testPath, strings.NewReader("hello updated"), "image/png")
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// 3. Download
		rc, err := bucket.Download(testPath)
		if err != nil {
			t.Fatalf("Download failed: %v", err)
		}
		if rc != nil {
			rc.Close()
		}

		// 4. GetPublicURL
		pubURL := bucket.GetPublicURL(testPath)
		expectedPubURL := server.URL + "/storage/v1/object/public/my-bucket/folder%20name/my%20file.png"
		if pubURL != expectedPubURL {
			t.Errorf("expected public URL %s, got %s", expectedPubURL, pubURL)
		}

		// 5. CreateSignedURL
		_, err = bucket.CreateSignedURL(testPath, 5*time.Minute)
		if err != nil {
			t.Fatalf("CreateSignedURL failed: %v", err)
		}
	})
}

func TestRealtimeClientSafety(t *testing.T) {
	client := NewClient(Config{
		URL:       "http://localhost:8080",
		AnonKey:   "anon",
		JWTSecret: "secret",
	})

	// Double Close safety
	t.Run("Double close does not panic", func(t *testing.T) {
		rc := client.Realtime
		rc.done = make(chan struct{})
		// Call close twice
		rc.Close()
		rc.Close()
	})
}
