package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func startTest(t *testing.T, cfg Config) (*Plugin, *ggrpc.ClientConn) {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	cfg.Quiet = true
	p := New(cfg)
	if err := p.Start(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown() })
	conn, err := ggrpc.NewClient(p.Addr().String(), ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return p, conn
}

func TestServesHealth(t *testing.T) {
	_, conn := startTest(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status %v", res.Status)
	}
}

func TestShutdownStopsServing(t *testing.T) {
	p, conn := startTest(t, Config{ShutdownTimeout: time.Second})
	if err := p.Shutdown(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err == nil {
		t.Fatal("expected the stopped server to refuse calls")
	}
}

func TestRecoverTurnsPanicIntoInternal(t *testing.T) {
	_, err := recoverUnary(context.Background(), nil, &ggrpc.UnaryServerInfo{FullMethod: "/x.Y/Z"}, func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("got %v", err)
	}
}

func TestBearerAuth(t *testing.T) {
	auth := BearerAuth(func(ctx context.Context, token string) (context.Context, error) {
		if token != "good" {
			return nil, errors.New("bad token")
		}
		return ctx, nil
	}, "/x.Y/Public")
	ok := func(ctx context.Context, _ any) (any, error) { return Token(ctx), nil }
	call := func(method string, md metadata.MD) (any, error) {
		ctx := metadata.NewIncomingContext(context.Background(), md)
		return auth(ctx, nil, &ggrpc.UnaryServerInfo{FullMethod: method}, ok)
	}
	if _, err := call("/x.Y/Z", metadata.MD{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing token: %v", err)
	}
	if _, err := call("/x.Y/Z", metadata.Pairs("authorization", "Bearer nope")); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("bad token: %v", err)
	}
	if v, err := call("/x.Y/Z", metadata.Pairs("authorization", "Bearer good")); err != nil || v != "good" {
		t.Fatalf("good token: %v %v", v, err)
	}
	if _, err := call("/x.Y/Public", metadata.MD{}); err != nil {
		t.Fatalf("public method: %v", err)
	}
	if _, err := call(healthpb.Health_Check_FullMethodName, metadata.MD{}); err != nil {
		t.Fatalf("health: %v", err)
	}
}
