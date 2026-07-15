package auth

import (
	"context"
	"testing"
	"time"
)

// ── Test User ───────────────────────────────────────────────────

type testUser struct {
	ID   string
	Name string
}

func (u *testUser) GetID() string { return u.ID }

// ── WithUser / UserFromContext ───────────────────────────────────

func TestWithUser_UserFromContext(t *testing.T) {
	user := &testUser{ID: "u1", Name: "Alice"}
	ctx := WithUser(context.Background(), user)

	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("expected user from context")
	}
	if got.GetID() != "u1" {
		t.Errorf("expected ID 'u1', got %q", got.GetID())
	}
}

func TestUserFromContext_NilWhenMissing(t *testing.T) {
	got := UserFromContext(context.Background())
	if got != nil {
		t.Error("expected nil for context without user")
	}
}

// ── SessionGuard (in-memory / legacy) ───────────────────────────

func TestSessionGuard_LoginLogout_Legacy(t *testing.T) {
	guard := NewSessionGuard()
	user := &testUser{ID: "u42", Name: "Bob"}

	// Login via legacy session_id context
	ctx := context.WithValue(context.Background(), "session_id", "sess-abc")
	_ = guard.Login(ctx, user)

	// User should be retrievable
	got, err := guard.User(ctx)
	if err != nil {
		t.Fatalf("User error: %v", err)
	}
	if got == nil || got.GetID() != "u42" {
		t.Errorf("expected user u42, got %v", got)
	}

	// Logout
	_ = guard.Logout(ctx)
	got, err = guard.User(ctx)
	if err != nil {
		t.Fatalf("User error after logout: %v", err)
	}
	if got != nil {
		t.Error("expected nil user after logout")
	}
}

func TestSessionGuard_UserReturnsNil_NoSession(t *testing.T) {
	guard := NewSessionGuard()
	got, err := guard.User(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil user without session")
	}
}

// ── SessionGuard with Loader ────────────────────────────────────

func TestSessionGuardWithLoader(t *testing.T) {
	loader := UserLoaderFunc(func(ctx context.Context, id string) (User, error) {
		if id == "u99" {
			return &testUser{ID: "u99", Name: "Loaded"}, nil
		}
		return nil, nil
	})
	guard := NewSessionGuardWithLoader(loader)

	// We can't easily test with real session middleware here,
	// but we verify the guard was created with a loader
	if guard.loader == nil {
		t.Error("expected loader to be set")
	}
}

// ── Gate ─────────────────────────────────────────────────────────

func TestGate_DefineAndAllows(t *testing.T) {
	gate := NewGate()
	gate.Define("edit-post", func(ctx context.Context, user User, resource any) bool {
		post := resource.(map[string]string)
		return user.GetID() == post["author_id"]
	})

	user := &testUser{ID: "u1"}
	ctx := context.Background()

	// Author can edit
	if !gate.Allows(ctx, user, "edit-post", map[string]string{"author_id": "u1"}) {
		t.Error("author should be allowed to edit own post")
	}

	// Non-author cannot
	if gate.Allows(ctx, user, "edit-post", map[string]string{"author_id": "u2"}) {
		t.Error("non-author should be denied")
	}
}

func TestGate_Denies(t *testing.T) {
	gate := NewGate()
	user := &testUser{ID: "u1"}
	ctx := context.Background()

	// Undefined ability defaults to deny
	if !gate.Denies(ctx, user, "nonexistent", nil) {
		t.Error("undefined ability should be denied")
	}
}

func TestGate_Authorize(t *testing.T) {
	gate := NewGate()
	gate.Define("view", func(ctx context.Context, user User, resource any) bool {
		return true
	})

	user := &testUser{ID: "u1"}
	ctx := context.Background()

	err := gate.Authorize(ctx, user, "view", nil)
	if err != nil {
		t.Errorf("expected authorized, got error: %v", err)
	}

	err = gate.Authorize(ctx, user, "delete", nil)
	if err == nil {
		t.Error("expected unauthorized error for undefined ability")
	}
}

func TestGate_Any(t *testing.T) {
	gate := NewGate()
	gate.Define("read", func(ctx context.Context, user User, resource any) bool {
		return true
	})

	user := &testUser{ID: "u1"}
	ctx := context.Background()

	if !gate.Any(ctx, user, []string{"read", "write"}, nil) {
		t.Error("Any should return true when at least one ability is allowed")
	}
}

func TestGate_None(t *testing.T) {
	gate := NewGate()
	user := &testUser{ID: "u1"}
	ctx := context.Background()

	if !gate.None(ctx, user, []string{"read", "write"}, nil) {
		t.Error("None should return true when no abilities are allowed")
	}
}

// ── Gate Before Hook ────────────────────────────────────────────

func TestGate_BeforeHook_AllowAll(t *testing.T) {
	gate := NewGate()
	// Admin override: always allow
	gate.Before(func(ctx context.Context, user User, ability string) *bool {
		if user.GetID() == "admin" {
			return AllowAll()
		}
		return nil
	})

	admin := &testUser{ID: "admin"}
	regular := &testUser{ID: "regular"}
	ctx := context.Background()

	// Admin allowed even for undefined abilities
	if !gate.Allows(ctx, admin, "anything", nil) {
		t.Error("admin should be allowed via Before hook")
	}

	// Regular user denied
	if gate.Allows(ctx, regular, "anything", nil) {
		t.Error("regular user should be denied for undefined ability")
	}
}

func TestGate_BeforeHook_DenyAll(t *testing.T) {
	gate := NewGate()
	gate.Define("read", func(ctx context.Context, user User, resource any) bool {
		return true
	})
	// Block suspended users
	gate.Before(func(ctx context.Context, user User, ability string) *bool {
		if user.GetID() == "suspended" {
			return DenyAll()
		}
		return nil
	})

	suspended := &testUser{ID: "suspended"}
	ctx := context.Background()

	if gate.Allows(ctx, suspended, "read", nil) {
		t.Error("suspended user should be denied via Before hook")
	}
}

// ── Gate After Hook ─────────────────────────────────────────────

func TestGate_AfterHook(t *testing.T) {
	gate := NewGate()
	gate.Define("read", func(ctx context.Context, user User, resource any) bool {
		return true
	})

	var logged []string
	gate.After(func(ctx context.Context, user User, ability string, result bool) {
		logged = append(logged, ability)
	})

	user := &testUser{ID: "u1"}
	ctx := context.Background()
	gate.Allows(ctx, user, "read", nil)

	if len(logged) != 1 || logged[0] != "read" {
		t.Errorf("expected after hook to log 'read', got %v", logged)
	}
}

// ── UserGate ────────────────────────────────────────────────────

func TestUserGate_Can_Cannot(t *testing.T) {
	gate := NewGate()
	gate.Define("publish", func(ctx context.Context, user User, resource any) bool {
		return user.GetID() == "editor"
	})

	editor := gate.ForUser(&testUser{ID: "editor"})
	viewer := gate.ForUser(&testUser{ID: "viewer"})
	ctx := context.Background()

	if !editor.Can(ctx, "publish", nil) {
		t.Error("editor should be able to publish")
	}
	if !viewer.Cannot(ctx, "publish", nil) {
		t.Error("viewer should not be able to publish")
	}
}

func TestUserGate_Authorize(t *testing.T) {
	gate := NewGate()
	gate.Define("edit", func(ctx context.Context, user User, resource any) bool {
		return true
	})

	ug := gate.ForUser(&testUser{ID: "u1"})
	ctx := context.Background()

	if err := ug.Authorize(ctx, "edit", nil); err != nil {
		t.Errorf("expected authorized, got: %v", err)
	}
}

// ── Default Gate (global) ───────────────────────────────────────

func TestDefaultGate_Can_Cannot(t *testing.T) {
	// Reset for test isolation
	defaultGate = NewGate()

	DefineAbility("test-ability", func(ctx context.Context, user User, resource any) bool {
		return true
	})

	user := &testUser{ID: "u1"}
	ctx := WithUser(context.Background(), user)

	if !Can(ctx, "test-ability", nil) {
		t.Error("expected Can to return true")
	}
	if Cannot(ctx, "test-ability", nil) {
		t.Error("expected Cannot to return false")
	}
}

func TestCan_NoUser_ReturnsFalse(t *testing.T) {
	defaultGate = NewGate()
	if Can(context.Background(), "anything", nil) {
		t.Error("Can should return false when no user in context")
	}
}

func TestAuthorizeAction_NoUser(t *testing.T) {
	defaultGate = NewGate()
	err := AuthorizeAction(context.Background(), "anything", nil)
	if err == nil {
		t.Error("expected unauthenticated error")
	}
}

// ── PersonalAccessToken ─────────────────────────────────────────

func TestPersonalAccessToken_HasAbility(t *testing.T) {
	tests := []struct {
		name      string
		abilities string
		check     string
		expected  bool
	}{
		{"wildcard", `["*"]`, "anything", true},
		{"empty string (default wildcard)", "", "anything", true},
		{"specific match", `["read:posts","write:posts"]`, "read:posts", true},
		{"specific no match", `["read:posts"]`, "write:posts", false},
		{"wildcard in list", `["read:posts","*"]`, "anything", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pat := &PersonalAccessToken{Abilities: tt.abilities}
			got := pat.HasAbility(tt.check)
			if got != tt.expected {
				t.Errorf("HasAbility(%q) = %v, want %v", tt.check, got, tt.expected)
			}
		})
	}
}

func TestPersonalAccessToken_HasAnyAllAbilities(t *testing.T) {
	pat := &PersonalAccessToken{Abilities: `["read:posts","write:posts"]`}

	if !pat.HasAnyAbility("delete:posts", "read:posts") {
		t.Error("HasAnyAbility should be true when one matches")
	}
	if pat.HasAnyAbility("delete:posts", "admin") {
		t.Error("HasAnyAbility should be false when none match")
	}
	if !pat.HasAllAbilities("read:posts", "write:posts") {
		t.Error("HasAllAbilities should be true when all match")
	}
	if pat.HasAllAbilities("read:posts", "delete:posts") {
		t.Error("HasAllAbilities should be false when one is missing")
	}

	// Wildcard grants both.
	wild := &PersonalAccessToken{Abilities: `["*"]`}
	if !wild.HasAnyAbility("a") || !wild.HasAllAbilities("a", "b", "c") {
		t.Error("wildcard token should satisfy any/all ability checks")
	}
}

func TestPersonalAccessToken_IsExpired(t *testing.T) {
	// Not expired (no expiry)
	pat := &PersonalAccessToken{}
	if pat.IsExpired() {
		t.Error("token without expiry should not be expired")
	}

	// Expired
	past := time.Now().Add(-time.Hour)
	pat2 := &PersonalAccessToken{ExpiresAt: &past}
	if !pat2.IsExpired() {
		t.Error("token with past expiry should be expired")
	}

	// Future expiry
	future := time.Now().Add(time.Hour)
	pat3 := &PersonalAccessToken{ExpiresAt: &future}
	if pat3.IsExpired() {
		t.Error("token with future expiry should not be expired")
	}
}

// ── Token Context Helpers ───────────────────────────────────────

func TestWithBearerToken(t *testing.T) {
	ctx := WithBearerToken(context.Background(), "my-token")
	got := tokenFromContext(ctx)
	if got != "my-token" {
		t.Errorf("expected 'my-token', got %q", got)
	}
}

func TestWithTokenRecord(t *testing.T) {
	pat := &PersonalAccessToken{ID: 42, Name: "test"}
	ctx := WithTokenRecord(context.Background(), pat)
	got := CurrentToken(ctx)
	if got == nil || got.ID != 42 {
		t.Errorf("expected token ID 42, got %v", got)
	}
}

func TestCurrentToken_NilWhenMissing(t *testing.T) {
	got := CurrentToken(context.Background())
	if got != nil {
		t.Error("expected nil token for empty context")
	}
}

// ── Helper Functions ────────────────────────────────────────────

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("test-token")
	h2 := hashToken("test-token")
	if h1 != h2 {
		t.Error("hashToken should be deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64 char hex SHA-256, got %d chars", len(h1))
	}
}

func TestGenerateToken_UniqueAndCorrectLength(t *testing.T) {
	t1, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken error: %v", err)
	}
	t2, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken error: %v", err)
	}
	if t1 == t2 {
		t.Error("generated tokens should be unique")
	}
	if len(t1) != 80 {
		t.Errorf("expected 80 char hex token (40 bytes), got %d chars", len(t1))
	}
}

func TestBoolPtr_AllowAll_DenyAll(t *testing.T) {
	allow := AllowAll()
	if allow == nil || !*allow {
		t.Error("AllowAll should return *true")
	}
	deny := DenyAll()
	if deny == nil || *deny {
		t.Error("DenyAll should return *false")
	}
}
