package ai

import (
	"context"
	"os"
	"testing"
)

type mockAIClient struct {
	PlanResponse     *PlanSummary
	ExecuteResponse  *MessageResponse
	RegenerateCalled bool
}

func (m *mockAIClient) Chat(ctx context.Context, prompt, model string, projCtx *ProjectContext) (string, error) {
	return "Mock AI reply for " + prompt, nil
}

func (m *mockAIClient) GeneratePlan(ctx context.Context, prompt string, projCtx *ProjectContext, model string) (*PlanSummary, error) {
	return m.PlanResponse, nil
}

func (m *mockAIClient) RegenerateStep(ctx context.Context, stepIndex int, newDesc string, currentPlan *PlanSummary, projCtx *ProjectContext, model string) (*PlanSummary, error) {
	m.RegenerateCalled = true
	currentPlan.Steps[stepIndex].Description = newDesc
	return currentPlan, nil
}

func (m *mockAIClient) StreamExecute(ctx context.Context, prompt string, plan *PlanSummary, messages []Message, tools []ToolDefinition, projCtx *ProjectContext, onDelta StreamHandler) (*MessageResponse, error) {
	if onDelta != nil {
		onDelta("Execution in progress...")
	}
	return m.ExecuteResponse, nil
}

// Turn is unsupported on this legacy mock so the agent exercises the
// plan/execute fallback path.
func (m *mockAIClient) Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	return nil, ErrTurnUnsupported
}

func TestAgentPlanningAndExecution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nimbus_agent_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockClient := &mockAIClient{
		PlanResponse: &PlanSummary{
			Summary: "Create Blog Model and Controller",
			Steps: []PlanStep{
				{
					ID:          1,
					Action:      "create_file",
					Target:      "app/models/blog.go",
					Description: "Define Blog struct with Title and Content",
					Risk:        "low",
				},
				{
					ID:          2,
					Action:      "run_command",
					Target:      "go build ./...",
					Description: "Verify compilation",
					Risk:        "low",
				},
			},
		},
		ExecuteResponse: &MessageResponse{
			Role: "assistant",
			Content: []ContentBlock{
				{
					Type: "text",
					Text: "All steps completed successfully. Blog model created.",
				},
			},
		},
	}

	tools := NewToolExecutor(tempDir)
	projCtx := &ProjectContext{AppRoot: tempDir, ProjectName: "test-app"}
	session := NewSession("optimal")

	agent := NewAgent(mockClient, tools, projCtx, session)
	ctx := context.Background()

	// 1. Test GeneratePlan
	plan, err := agent.GeneratePlan(ctx, "create a blog model")
	if err != nil {
		t.Fatalf("GeneratePlan failed: %v", err)
	}
	if plan.Summary != "Create Blog Model and Controller" {
		t.Errorf("unexpected plan summary: %s", plan.Summary)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if agent.State != StateReviewing {
		t.Errorf("expected state StateReviewing, got %s", agent.State)
	}

	// 2. Test RegenerateStep
	updatedPlan, err := agent.RegenerateStep(ctx, 0, "Define Blog struct with Title, Content and Slug")
	if err != nil {
		t.Fatalf("RegenerateStep failed: %v", err)
	}
	if !mockClient.RegenerateCalled {
		t.Error("expected RegenerateStep to be called on client")
	}
	if updatedPlan.Steps[0].Description != "Define Blog struct with Title, Content and Slug" {
		t.Errorf("expected updated description, got %s", updatedPlan.Steps[0].Description)
	}

	// 3. Test ExecuteApprovedPlan
	summary, err := agent.ExecuteApprovedPlan(ctx, updatedPlan)
	if err != nil {
		t.Fatalf("ExecuteApprovedPlan failed: %v", err)
	}
	if summary != "All steps completed successfully. Blog model created." {
		t.Errorf("unexpected summary: %s", summary)
	}
	if agent.State != StateCompleted {
		t.Errorf("expected state StateCompleted, got %s", agent.State)
	}
}
