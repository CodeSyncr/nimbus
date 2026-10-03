package auth

import (
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func assertSingleConsumer(t *testing.T, a, b *TokenStore) {
	t.Helper()
	tok, err := a.Create("u")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			s := a
			if i%2 == 0 {
				s = b
			}
			if s.Verify("u", tok) {
				accepted.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d times", accepted.Load())
	}
	token, err := a.Create("u")
	if err != nil {
		t.Fatal(err)
	}
	if b.Verify("u", "wrong") {
		t.Fatal("wrong token accepted")
	}
	if !b.Verify("u", token) {
		t.Fatal("wrong token destroyed valid token")
	}
}
func TestTokenConsumptionAtomic(t *testing.T) {
	s := NewTokenStore("test", time.Minute)
	assertSingleConsumer(t, s, s)
}
func TestRedisTokenConsumptionAcrossInstances(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	a := NewRedisTokenStore(client, "reset:", "test", time.Minute)
	b := NewRedisTokenStore(client, "reset:", "test", time.Minute)
	assertSingleConsumer(t, a, b)
	token, err := a.Create("u")
	if err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Minute)
	if b.Verify("u", token) {
		t.Fatal("expired token accepted")
	}
	server.Close()
	if _, err := b.VerifyWithError("u", token); err == nil {
		t.Fatal("store failure hidden")
	}
}
