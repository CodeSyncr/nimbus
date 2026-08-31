package captcha

import (
	"context"
	"errors"
	"sync"

	"github.com/CodeSyncr/nimbus/container"
)

var (
	// ErrMockSolveFailed is returned when mock solver fails intentionally in tests.
	ErrMockSolveFailed = errors.New("captcha: mock solve failed")
	// ErrPluginNotRegistered is returned when captcha facade is called before plugin registration.
	ErrPluginNotRegistered = errors.New("captcha: plugin not registered. Call app.Use(captcha.New())")

	globalManager *Manager
	mu            sync.RWMutex
)

// Manager coordinates Captcha Client and Verifier operations.
type Manager struct {
	config   *Config
	Client   *Client
	Verifier *Verifier
}

// NewManager creates a Manager instance from configuration.
func NewManager(cfg *Config) (*Manager, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}

	verifier := NewVerifier(cfg)

	return &Manager{
		config:   cfg,
		Client:   client,
		Verifier: verifier,
	}, nil
}

// setManager registers global manager instance for facade functions.
func setManager(m *Manager) {
	mu.Lock()
	defer mu.Unlock()
	globalManager = m
}

// GetManager returns global manager instance.
func GetManager() *Manager {
	mu.RLock()
	defer mu.RUnlock()
	return globalManager
}

// Bindings registers captcha manager into container.
func Bindings(c *container.Container, m *Manager) {
	c.Singleton("captcha", func() *Manager { return m })
	c.Singleton("captcha.client", func() *Client { return m.Client })
	c.Singleton("captcha.verifier", func() *Verifier { return m.Verifier })
}

// ═══════════════════════════════════════════════════════════════════
// Package Facades (Package Level Helpers)
// ═══════════════════════════════════════════════════════════════════

// Solve solves a captcha challenge task programmatically via Nimbus Cloud (CapSolver alternative).
func Solve(ctx context.Context, payload TaskPayload) (*Solution, error) {
	mgr := GetManager()
	if mgr == nil {
		return nil, ErrPluginNotRegistered
	}
	return mgr.Client.Solve(ctx, payload)
}

// CreateTask submits a captcha task to Nimbus Cloud.
func CreateTask(ctx context.Context, payload TaskPayload) (*CreateTaskResponse, error) {
	mgr := GetManager()
	if mgr == nil {
		return nil, ErrPluginNotRegistered
	}
	return mgr.Client.CreateTask(ctx, payload)
}

// GetTaskResult gets status or solution of an async task.
func GetTaskResult(ctx context.Context, taskID string) (*GetTaskResultResponse, error) {
	mgr := GetManager()
	if mgr == nil {
		return nil, ErrPluginNotRegistered
	}
	return mgr.Client.GetTaskResult(ctx, taskID)
}

// GetBalance checks remaining credits on Nimbus Cloud.
func GetBalance(ctx context.Context) (float64, error) {
	mgr := GetManager()
	if mgr == nil {
		return 0, ErrPluginNotRegistered
	}
	return mgr.Client.GetBalance(ctx)
}

// Verify validates a submitted user captcha token.
func Verify(ctx context.Context, provider, token, remoteIP string) (*VerificationResult, error) {
	mgr := GetManager()
	if mgr == nil {
		return nil, ErrPluginNotRegistered
	}
	return mgr.Verifier.VerifyToken(ctx, provider, token, remoteIP)
}
