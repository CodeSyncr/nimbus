/*
|--------------------------------------------------------------------------
| gRPC — serve gRPC services next to HTTP, from the same binary
|--------------------------------------------------------------------------
|
| Optional plugin. Only apps that import it link the gRPC library.
|
|   app.Use(nimbusgrpc.New(nimbusgrpc.Config{
|       Register: func(s *grpc.Server) {
|           pb.RegisterGreeterServer(s, &services.Greeter{})
|       },
|   }))
|
| The server starts with the app, on its own port, and stops gracefully on
| shutdown. Every server gets panic recovery, one log line per call, the
| standard health service (grpc.health.v1) and, outside production, server
| reflection so grpcurl and Postman can list the services.
|
| Env:
|   GRPC_ADDR=:50051        where to listen
|   GRPC_REFLECTION=true    force reflection on or off
|
| Generate a service with: nimbus make:grpc Greeter
|
*/

package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var (
	_ nimbus.Plugin      = (*Plugin)(nil)
	_ nimbus.HasStart    = (*Plugin)(nil)
	_ nimbus.HasShutdown = (*Plugin)(nil)
)

// Config configures the gRPC server.
type Config struct {
	// Addr is where the server listens. Default GRPC_ADDR, then ":50051".
	Addr string
	// Register adds your services to the server. Called once, before it starts.
	Register func(s *ggrpc.Server)
	// Reflection exposes the service list to tools like grpcurl. Nil means on
	// outside production (APP_ENV != "production"); GRPC_REFLECTION overrides.
	Reflection *bool
	// Unary and Stream interceptors run after the built-in recovery and
	// logging ones, in order.
	Unary  []ggrpc.UnaryServerInterceptor
	Stream []ggrpc.StreamServerInterceptor
	// Options are passed to grpc.NewServer as they are (TLS credentials,
	// message size limits, keepalive).
	Options []ggrpc.ServerOption
	// ShutdownTimeout bounds the graceful stop; after it, open calls are
	// cancelled. Default 10s.
	ShutdownTimeout time.Duration
	// Logger receives one line per call. Default log.Printf; set Quiet to
	// turn call logs off.
	Logger func(format string, args ...any)
	Quiet  bool
}

// Plugin runs a gRPC server alongside the HTTP server.
type Plugin struct {
	nimbus.BasePlugin
	cfg    Config
	mu     sync.Mutex
	server *ggrpc.Server
	health *health.Server
	addr   net.Addr
}

// New creates the gRPC plugin. Settings left empty come from the
// environment, then from the defaults above.
func New(cfg Config) *Plugin {
	if cfg.Addr == "" {
		cfg.Addr = os.Getenv("GRPC_ADDR")
	}
	if cfg.Addr == "" {
		cfg.Addr = ":50051"
	}
	if v := os.Getenv("GRPC_REFLECTION"); v != "" {
		on, err := strconv.ParseBool(v)
		if err == nil {
			cfg.Reflection = &on
		}
	}
	if cfg.Reflection == nil {
		on := !strings.EqualFold(os.Getenv("APP_ENV"), "production")
		cfg.Reflection = &on
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Printf
	}
	return &Plugin{cfg: cfg}
}

// Name implements nimbus.Plugin.
func (p *Plugin) Name() string { return "grpc" }

// Version implements nimbus.Plugin.
func (p *Plugin) Version() string { return "1.0.0" }

// Server builds the gRPC server with the built-in interceptors, health and
// reflection, and your services. Start calls it; tests can call it directly.
func (p *Plugin) Server() *ggrpc.Server {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil {
		return p.server
	}
	unary := append([]ggrpc.UnaryServerInterceptor{recoverUnary, p.logUnary}, p.cfg.Unary...)
	stream := append([]ggrpc.StreamServerInterceptor{recoverStream, p.logStream}, p.cfg.Stream...)
	opts := append([]ggrpc.ServerOption{ggrpc.ChainUnaryInterceptor(unary...), ggrpc.ChainStreamInterceptor(stream...)}, p.cfg.Options...)
	s := ggrpc.NewServer(opts...)
	p.health = health.NewServer()
	healthpb.RegisterHealthServer(s, p.health)
	if p.cfg.Register != nil {
		p.cfg.Register(s)
	}
	if *p.cfg.Reflection {
		reflection.Register(s)
	}
	for name := range s.GetServiceInfo() {
		p.health.SetServingStatus(name, healthpb.HealthCheckResponse_SERVING)
	}
	p.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	p.server = s
	return s
}

// Start implements nimbus.HasStart: it listens and serves in the background.
func (p *Plugin) Start(*nimbus.App) error {
	ln, err := net.Listen("tcp", p.cfg.Addr)
	if err != nil {
		return fmt.Errorf("grpc: listen %s: %w", p.cfg.Addr, err)
	}
	s := p.Server()
	p.mu.Lock()
	p.addr = ln.Addr()
	p.mu.Unlock()
	p.cfg.Logger("[nimbus] gRPC listening on %s", ln.Addr())
	go func() {
		if err := s.Serve(ln); err != nil && err != ggrpc.ErrServerStopped {
			p.cfg.Logger("[nimbus] gRPC server stopped: %v", err)
		}
	}()
	return nil
}

// Addr reports where the server is listening once it has started (useful
// with Addr ":0" in tests).
func (p *Plugin) Addr() net.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addr
}

// Shutdown implements nimbus.HasShutdown: health turns NOT_SERVING, open
// calls get ShutdownTimeout to finish, then the rest are cancelled.
func (p *Plugin) Shutdown() error {
	p.mu.Lock()
	s, h := p.server, p.health
	p.mu.Unlock()
	if s == nil {
		return nil
	}
	if h != nil {
		h.Shutdown()
	}
	done := make(chan struct{})
	go func() { s.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(p.cfg.ShutdownTimeout):
		s.Stop()
	}
	return nil
}

// ── built-in interceptors ───────────────────────────────────────────────

// A panicking handler answers codes.Internal instead of taking the process
// down, and the stack is logged.
func recoverUnary(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[nimbus] gRPC panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

func recoverStream(srv any, ss ggrpc.ServerStream, info *ggrpc.StreamServerInfo, handler ggrpc.StreamHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[nimbus] gRPC panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(srv, ss)
}

func (p *Plugin) logUnary(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	if !p.cfg.Quiet && info.FullMethod != healthpb.Health_Check_FullMethodName {
		p.cfg.Logger("[nimbus] gRPC %s %s %s", info.FullMethod, status.Code(err), time.Since(start).Round(time.Microsecond))
	}
	return resp, err
}

func (p *Plugin) logStream(srv any, ss ggrpc.ServerStream, info *ggrpc.StreamServerInfo, handler ggrpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	if !p.cfg.Quiet {
		p.cfg.Logger("[nimbus] gRPC stream %s %s %s", info.FullMethod, status.Code(err), time.Since(start).Round(time.Microsecond))
	}
	return err
}

// ── bearer-token auth ───────────────────────────────────────────────────

// TokenFunc checks a bearer token and returns the context the handler should
// run with (for example with the user attached), or an error.
type TokenFunc func(ctx context.Context, token string) (context.Context, error)

type tokenKey struct{}

// Token returns the bearer token the call was authorised with, if any.
func Token(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey{}).(string)
	return t
}

// BearerAuth returns a unary interceptor that requires
// "authorization: Bearer <token>" metadata on every call except the
// methods listed in public (full names, such as "/greeter.Greeter/Ping")
// and the health service. A missing or rejected token answers
// codes.Unauthenticated.
func BearerAuth(check TokenFunc, public ...string) ggrpc.UnaryServerInterceptor {
	open := map[string]bool{healthpb.Health_Check_FullMethodName: true, healthpb.Health_Watch_FullMethodName: true}
	for _, m := range public {
		open[m] = true
	}
	return func(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
		if open[info.FullMethod] {
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		var token string
		for _, v := range md.Get("authorization") {
			if t, ok := strings.CutPrefix(v, "Bearer "); ok {
				token = strings.TrimSpace(t)
				break
			}
		}
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		next, err := check(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		if next == nil {
			next = ctx
		}
		return handler(context.WithValue(next, tokenKey{}, token), req)
	}
}
