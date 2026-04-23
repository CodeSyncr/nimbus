package queue

import "testing"

func TestBootWithErrorRejectsUnknownDriverInStrictMode(t *testing.T) {
	_, err := BootWithError(&BootConfig{
		Driver: "not-a-driver",
		Strict: true,
	})
	if err == nil {
		t.Fatal("expected error for unknown queue driver in strict mode")
	}
}

func TestBootWithErrorRejectsMissingDatabaseConnection(t *testing.T) {
	_, err := BootWithError(&BootConfig{
		Driver: "database",
	})
	if err == nil {
		t.Fatal("expected error when database queue is configured without connection")
	}
}

