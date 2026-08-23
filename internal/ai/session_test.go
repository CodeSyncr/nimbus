package ai

import (
	"os"
	"testing"
)

func TestSessionPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nimbus_session_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	session := NewSession("optimal")
	session.InitialQuery = "add payments checkout"
	session.Plan = &PlanSummary{
		Summary: "Scaffold cashier and checkout controller",
		Steps: []PlanStep{
			{
				ID:          1,
				Action:      "create_file",
				Target:      "app/controllers/checkout_controller.go",
				Description: "Handle checkout sessions",
				Risk:        "low",
				Approved:    true,
			},
		},
	}

	// 1. Save session
	if err := SaveSession(tempDir, session); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// 2. Load session
	loaded, err := LoadSession(tempDir, session.ID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if loaded.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, loaded.ID)
	}
	if loaded.InitialQuery != "add payments checkout" {
		t.Errorf("expected initial query 'add payments checkout', got %s", loaded.InitialQuery)
	}
	if len(loaded.Plan.Steps) != 1 {
		t.Fatalf("expected 1 plan step, got %d", len(loaded.Plan.Steps))
	}

	// 3. List sessions
	list, err := ListSessions(tempDir)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != session.ID {
		t.Errorf("expected list to contain 1 session with ID %s", session.ID)
	}
}
