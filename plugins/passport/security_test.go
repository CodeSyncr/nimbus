package passport

import (
	"context"
	"errors"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/plugins/passport/models"
	"gorm.io/driver/sqlite"
	"path/filepath"
	"sync"
	"testing"
)

func securityServer(t *testing.T) (*Server, *NewClientResult, string) {
	t.Helper()
	db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "oauth.db")+"?_busy_timeout=5000"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	for _, m := range models.Migrations() {
		if err := m.Up(db); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(db, Config{})
	ctx := context.Background()
	c, err := s.CreateClient(ctx, "test", "owner", []string{"https://app.test/cb"}, []string{GrantAuthorizationCode, GrantRefreshToken}, []string{"read"}, true)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.ValidateAuthorize(ctx, c.Client.ClientID, "code", "https://app.test/cb", "read", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.IssueAuthCode(ctx, req, "user")
	if err != nil {
		t.Fatal(err)
	}
	return s, c, code
}
func TestOAuthConcurrentConsumption(t *testing.T) {
	s, c, code := securityServer(t)
	ctx := context.Background()
	consume := func(run func(*Server) (*TokenResponse, error)) *TokenResponse {
		t.Helper()
		var wg sync.WaitGroup
		results := make(chan *TokenResponse, 12)
		start := make(chan struct{})
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				peer := NewServer(s.db, Config{})
				r, err := run(peer)
				if err == nil {
					results <- r
				} else if !errors.Is(err, ErrInvalidGrant) {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		if len(results) != 1 {
			t.Fatalf("successful consumers=%d", len(results))
		}
		return <-results
	}
	pair := consume(func(s *Server) (*TokenResponse, error) {
		return s.ExchangeAuthCode(ctx, c.Client.ClientID, c.PlainSecret, code, "https://app.test/cb", "")
	})
	next := consume(func(s *Server) (*TokenResponse, error) {
		return s.Refresh(ctx, c.Client.ClientID, c.PlainSecret, pair.RefreshToken, "")
	})
	if _, err := s.ValidateAccessToken(ctx, pair.AccessToken); err == nil {
		t.Fatal("old access token active")
	}
	if _, err := s.ValidateAccessToken(ctx, next.AccessToken); err != nil {
		t.Fatal(err)
	}
}
func TestOAuthIssuanceFailureRollsBack(t *testing.T) {
	for _, phase := range []string{"code", "refresh"} {
		t.Run(phase, func(t *testing.T) {
			s, c, code := securityServer(t)
			ctx := context.Background()
			var pair *TokenResponse
			var err error
			if phase == "refresh" {
				pair, err = s.ExchangeAuthCode(ctx, c.Client.ClientID, c.PlainSecret, code, "https://app.test/cb", "")
				if err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("injected refresh write failure")
			name := "test:fail-refresh"
			err = s.db.Callback().Create().Before("gorm:create").Register(name, func(tx *lucid.DB) {
				if tx.Statement.Table == "oauth_refresh_tokens" {
					tx.AddError(failure)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() (*TokenResponse, error) {
				if phase == "code" {
					return s.ExchangeAuthCode(ctx, c.Client.ClientID, c.PlainSecret, code, "https://app.test/cb", "")
				}
				return s.Refresh(ctx, c.Client.ClientID, c.PlainSecret, pair.RefreshToken, "")
			}
			if got, err := invoke(); err == nil || got != nil {
				t.Fatal("credentials returned despite failed write")
			}
			var count int64
			s.db.Model(&models.OAuthAccessToken{}).Count(&count)
			want := int64(0)
			if phase == "refresh" {
				want = 1
			}
			if count != want {
				t.Fatalf("partial access token persisted: %d", count)
			}
			if pair != nil {
				if _, err := s.ValidateAccessToken(ctx, pair.AccessToken); err != nil {
					t.Fatal("failed rotation revoked original")
				}
			}
			if err := s.db.Callback().Create().Remove(name); err != nil {
				t.Fatal(err)
			}
			if _, err := invoke(); err != nil {
				t.Fatalf("rollback did not restore token: %v", err)
			}
		})
	}
}
