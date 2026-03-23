package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testJob struct {
	ShouldFail bool `json:"should_fail"`
}

func (j *testJob) Handle(ctx context.Context) error {
	if j.ShouldFail {
		return errors.New("job failed")
	}
	return nil
}

type stubCompletableAdapter struct {
	popPayload *JobPayload
	pushes     []*JobPayload
	completes  int
}

func (s *stubCompletableAdapter) Push(ctx context.Context, payload *JobPayload) error {
	cp := *payload
	s.pushes = append(s.pushes, &cp)
	return nil
}

func (s *stubCompletableAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	p := s.popPayload
	s.popPayload = nil
	return p, nil
}

func (s *stubCompletableAdapter) Len(ctx context.Context, queue string) (int, error) {
	return 0, nil
}

func (s *stubCompletableAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	s.completes++
	return nil
}

func TestManagerProcessAckOnSuccess(t *testing.T) {
	ctx := context.Background()
	adapter := &stubCompletableAdapter{}
	m := NewManager(adapter)
	m.Register(&testJob{})

	payload, err := m.Dispatch(&testJob{}).DispatchBuilderForTest()
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}
	adapter.popPayload = payload

	if err := m.Process(ctx, "default"); err != nil {
		t.Fatalf("process success job: %v", err)
	}
	if adapter.completes != 1 {
		t.Fatalf("expected 1 complete call, got %d", adapter.completes)
	}
	if len(adapter.pushes) != 0 {
		t.Fatalf("expected no retries, got %d pushes", len(adapter.pushes))
	}
}

func TestManagerProcessAckOnRetry(t *testing.T) {
	ctx := context.Background()
	adapter := &stubCompletableAdapter{}
	m := NewManager(adapter)
	m.Register(&testJob{})

	payload, err := m.Dispatch(&testJob{ShouldFail: true}).Retries(1).DispatchBuilderForTest()
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}
	adapter.popPayload = payload

	if err := m.Process(ctx, "default"); err != nil {
		t.Fatalf("process retry job: %v", err)
	}
	if adapter.completes != 1 {
		t.Fatalf("expected 1 complete call on retry path, got %d", adapter.completes)
	}
	if len(adapter.pushes) != 1 {
		t.Fatalf("expected 1 retry push, got %d", len(adapter.pushes))
	}
	if adapter.pushes[0].Attempts != 1 {
		t.Fatalf("expected attempts=1 after retry, got %d", adapter.pushes[0].Attempts)
	}
	if adapter.pushes[0].Delay <= 0 || time.Until(adapter.pushes[0].RunAt) <= 0 {
		t.Fatalf("expected retry delay/runAt to be set")
	}
}

func TestManagerProcessAckOnPermanentFailure(t *testing.T) {
	ctx := context.Background()
	adapter := &stubCompletableAdapter{}
	m := NewManager(adapter)
	m.Register(&testJob{})

	payload, err := m.Dispatch(&testJob{ShouldFail: true}).Retries(0).DispatchBuilderForTest()
	if err != nil {
		t.Fatalf("serialize payload: %v", err)
	}
	adapter.popPayload = payload

	if err := m.Process(ctx, "default"); err == nil {
		t.Fatal("expected permanent failure error")
	}
	if adapter.completes != 1 {
		t.Fatalf("expected 1 complete call on terminal failure, got %d", adapter.completes)
	}
}

// DispatchBuilderForTest exposes serialization for test payload setup.
func (b *DispatchBuilder) DispatchBuilderForTest() (*JobPayload, error) {
	return b.serialize()
}
