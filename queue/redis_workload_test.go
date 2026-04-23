package queue

import (
	"context"
	"testing"
)

func TestRedisQueueWorkloadsNilClient(t *testing.T) {
	_, err := RedisQueueWorkloads(context.Background(), nil, []string{"default"})
	if err == nil {
		t.Fatal("expected error for nil redis client")
	}
}
