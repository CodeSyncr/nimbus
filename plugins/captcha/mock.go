package captcha

import (
	"context"
	"time"
)

// MockSolver provides mock responses for unit testing without external network calls.
type MockSolver struct {
	ShouldFail bool
	MockToken  string
}

// NewMockSolver creates a new MockSolver.
func NewMockSolver() *MockSolver {
	return &MockSolver{
		ShouldFail: false,
		MockToken:  "mock-captcha-token-approved",
	}
}

// Solve returns a pre-configured mock Solution.
func (m *MockSolver) Solve(ctx context.Context, payload TaskPayload) (*Solution, error) {
	if m.ShouldFail {
		return nil, ErrMockSolveFailed
	}

	text := ""
	if payload.Type == TaskTypeImageToText {
		text = "MOCK_OCR_TEXT"
	}

	return &Solution{
		Token:     m.MockToken,
		Text:      text,
		UserAgent: "NimbusMockAgent/1.0",
		SolveTime: 50 * time.Millisecond,
	}, nil
}
