package nimbus_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/events"
	nhttp "github.com/CodeSyncr/nimbus/http"
	ntest "github.com/CodeSyncr/nimbus/testing"
)

type dummyProvider struct {
	registerCalled bool
	bootCalled     bool
	modeAtBoot     nimbus.AppMode
}

func (d *dummyProvider) Register(app *nimbus.App) error {
	d.registerCalled = true
	return nil
}

func (d *dummyProvider) Boot(app *nimbus.App) error {
	d.bootCalled = true
	d.modeAtBoot = app.GetMode()
	return nil
}

type dummyPluginWithShutdown struct {
	name           string
	shutdownCalled bool
}

func (p *dummyPluginWithShutdown) Name() string {
	if p.name == "" {
		return "dummy-shutdown"
	}
	return p.name
}

func (p *dummyPluginWithShutdown) Version() string {
	return "1.0.0"
}

func (p *dummyPluginWithShutdown) Register(app *nimbus.App) error {
	return nil
}

func (p *dummyPluginWithShutdown) Boot(app *nimbus.App) error {
	return nil
}

func (p *dummyPluginWithShutdown) Shutdown() error {
	p.shutdownCalled = true
	return nil
}

func TestApp_InitialStateAndDefaultMode(t *testing.T) {
	app := nimbus.New()

	if mode := app.GetMode(); mode != nimbus.ModeRun {
		t.Fatalf("expected default mode %q, got %q", nimbus.ModeRun, mode)
	}
	if app.IsWarmup() {
		t.Fatalf("expected IsWarmup() to be false by default")
	}
	if state := app.State(); state != nimbus.StateInitiated {
		t.Fatalf("expected initial state %q, got %q", nimbus.StateInitiated, state)
	}
	if app.IsBooted() {
		t.Fatalf("expected IsBooted() to be false before Boot/WarmUp")
	}
	if app.IsWarmedUp() {
		t.Fatalf("expected IsWarmedUp() to be false before WarmUp")
	}
}

func TestApp_SetModeAndAccessors(t *testing.T) {
	app := nimbus.New()

	app.SetMode(nimbus.ModeWarmup)
	if app.GetMode() != nimbus.ModeWarmup {
		t.Fatalf("expected mode %q, got %q", nimbus.ModeWarmup, app.GetMode())
	}
	if app.Mode() != nimbus.ModeWarmup {
		t.Fatalf("expected Mode() %q, got %q", nimbus.ModeWarmup, app.Mode())
	}
	if !app.IsWarmup() {
		t.Fatalf("expected IsWarmup() to be true")
	}

	app.SetMode(nimbus.ModeTest)
	if app.GetMode() != nimbus.ModeTest {
		t.Fatalf("expected mode %q, got %q", nimbus.ModeTest, app.GetMode())
	}
	if app.IsWarmup() {
		t.Fatalf("expected IsWarmup() to be false in test mode")
	}
}

func TestApp_WarmUp(t *testing.T) {
	app := nimbus.New()
	provider := &dummyProvider{}
	app.Register(provider)

	warmHookRan := false
	app.OnWarmup(func(a *nimbus.App) {
		warmHookRan = true
		if a.State() != nimbus.StateWarming {
			t.Errorf("expected state %q during OnWarmup, got %q", nimbus.StateWarming, a.State())
		}
	})

	var eventAppWarmedReceived bool
	app.Events.Listen(events.AppWarmed, func(payload any) error {
		eventAppWarmedReceived = true
		if _, ok := payload.(*nimbus.App); !ok {
			t.Errorf("expected *nimbus.App payload for app:warmed, got %T", payload)
		}
		return nil
	})

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	if !provider.registerCalled {
		t.Errorf("expected provider Register to have been called")
	}
	if !provider.bootCalled {
		t.Errorf("expected provider Boot to have been called")
	}
	if !warmHookRan {
		t.Errorf("expected OnWarmup hook to have executed")
	}
	if !eventAppWarmedReceived {
		t.Errorf("expected events.AppWarmed event to have been dispatched")
	}
	if !app.IsBooted() {
		t.Errorf("expected IsBooted() to be true after WarmUp")
	}
	if !app.IsWarmedUp() {
		t.Errorf("expected IsWarmedUp() to be true after WarmUp")
	}
	if app.State() != nimbus.StateWarmed {
		t.Errorf("expected state %q after WarmUp, got %q", nimbus.StateWarmed, app.State())
	}
}

func TestApp_WarmUpIdempotency(t *testing.T) {
	app := nimbus.New()

	warmCount := 0
	bootCount := 0
	app.OnBoot(func(a *nimbus.App) {
		bootCount++
	})
	app.OnWarmup(func(a *nimbus.App) {
		warmCount++
	})

	if err := app.WarmUp(); err != nil {
		t.Fatalf("first WarmUp failed: %v", err)
	}
	if err := app.WarmUp(); err != nil {
		t.Fatalf("second WarmUp failed: %v", err)
	}

	if bootCount != 1 {
		t.Errorf("expected OnBoot to run once, ran %d times", bootCount)
	}
	if warmCount != 1 {
		t.Errorf("expected OnWarmup to run once, ran %d times", warmCount)
	}
}

func TestApp_HookExecutionOrder(t *testing.T) {
	app := nimbus.New()

	var order []string
	app.OnBoot(func(a *nimbus.App) {
		order = append(order, "boot")
	})
	app.OnWarmup(func(a *nimbus.App) {
		order = append(order, "warmup")
	})

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	if len(order) != 2 || order[0] != "boot" || order[1] != "warmup" {
		t.Fatalf("expected order [boot, warmup], got %v", order)
	}
}

func TestApp_RunRejectedInWarmupMode(t *testing.T) {
	app := nimbus.New()
	app.SetMode(nimbus.ModeWarmup)

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	err := app.Run()
	if err == nil {
		t.Fatalf("expected Run() to fail when app is in ModeWarmup")
	}

	errTLS := app.RunTLS("cert.pem", "key.pem")
	if errTLS == nil {
		t.Fatalf("expected RunTLS() to fail when app is in ModeWarmup")
	}
}

func TestApp_ShutdownInWarmupMode(t *testing.T) {
	app := nimbus.New()
	app.SetMode(nimbus.ModeWarmup)

	plugin := &dummyPluginWithShutdown{name: "test-shutdown-plugin"}
	app.Use(plugin)

	shutdownHookRan := false
	app.OnShutdown(func(a *nimbus.App) {
		shutdownHookRan = true
	})

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if !shutdownHookRan {
		t.Errorf("expected shutdownHook to run even in warmup mode")
	}
	if plugin.shutdownCalled {
		t.Errorf("expected plugin HasShutdown to be skipped in warmup mode to avoid lazy resource construction")
	}
	if app.State() != nimbus.StateTerminated {
		t.Errorf("expected state %q after Shutdown, got %q", nimbus.StateTerminated, app.State())
	}
}

func TestApp_ShutdownInRunMode(t *testing.T) {
	app := nimbus.New()
	app.SetMode(nimbus.ModeRun)

	plugin := &dummyPluginWithShutdown{name: "test-shutdown-plugin"}
	app.Use(plugin)

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if !plugin.shutdownCalled {
		t.Errorf("expected plugin HasShutdown to be called in ModeRun")
	}
	if app.State() != nimbus.StateTerminated {
		t.Errorf("expected state %q after Shutdown, got %q", nimbus.StateTerminated, app.State())
	}
}

func TestApp_ProviderModeAwareness(t *testing.T) {
	app := nimbus.New()
	app.SetMode(nimbus.ModeWarmup)

	provider := &dummyProvider{}
	app.Register(provider)

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	if provider.modeAtBoot != nimbus.ModeWarmup {
		t.Errorf("expected provider to observe mode %q at boot, got %q", nimbus.ModeWarmup, provider.modeAtBoot)
	}
}

func TestApp_FunctionalOptions(t *testing.T) {
	app := nimbus.New(
		nimbus.WithMode(nimbus.ModeTest),
		nimbus.WithPort("8888"),
	)

	if !app.IsTest() {
		t.Errorf("expected IsTest() to be true")
	}
	if app.IsRun() || app.IsWarmup() || app.IsCli() {
		t.Errorf("expected only IsTest() to be true")
	}
	if app.Config.App.Port != "8888" {
		t.Errorf("expected port to be 8888, got %s", app.Config.App.Port)
	}
}

func TestApp_ServeHTTPDirectly(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeTest))
	app.Router.Get("/ping", func(c *nhttp.Context) error {
		c.String(stdhttp.StatusOK, "pong")
		return nil
	})

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	req := httptest.NewRequest(stdhttp.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()

	// Direct ServeHTTP call on *App
	app.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Errorf("expected body 'pong', got %q", rec.Body.String())
	}
}

type dummyProviderWithLifecycle struct {
	dummyProvider
	startCalled    bool
	shutdownCalled bool
}

func (d *dummyProviderWithLifecycle) Start(app *nimbus.App) error {
	d.startCalled = true
	return nil
}

func (d *dummyProviderWithLifecycle) Shutdown() error {
	d.shutdownCalled = true
	return nil
}

func TestApp_ProviderLifecycleHooks(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeRun))
	p := &dummyProviderWithLifecycle{}
	app.Register(p)

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	// In warmup phase, Start is not called yet
	if p.startCalled {
		t.Errorf("expected Provider Start to not be called during WarmUp")
	}

	// Test shutdown in ModeRun
	if err := app.Shutdown(); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
	if !p.shutdownCalled {
		t.Errorf("expected Provider Shutdown to be called in ModeRun")
	}
}

func TestApp_DumpRoutesProgrammatically(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeWarmup))
	app.Router.Get("/api/items", func(c *nhttp.Context) error {
		return c.JSON(stdhttp.StatusOK, map[string]string{"status": "ok"})
	})

	tmpDir := t.TempDir()
	if err := app.DumpRoutes(tmpDir); err != nil {
		t.Fatalf("DumpRoutes failed: %v", err)
	}

	if !app.IsWarmedUp() {
		t.Errorf("expected app to be warmed up after DumpRoutes")
	}
}

func TestApp_ConcurrentAccess(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeWarmup))

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = app.GetMode()
				_ = app.State()
				_ = app.IsWarmedUp()
				_ = app.IsBooted()
				_ = app.IsWarmup()
			}
		}()
	}

	if err := app.WarmUp(); err != nil {
		t.Fatalf("WarmUp failed: %v", err)
	}

	wg.Wait()

	if !app.IsWarmedUp() {
		t.Errorf("expected app to be warmed up")
	}
}

func TestApp_DumpOpenAPI(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeWarmup))
	app.Router.Get("/users", func(c *nhttp.Context) error {
		return c.JSON(stdhttp.StatusOK, map[string]string{"user": "alice"})
	})

	tmpFile := t.TempDir() + "/spec.json"
	if err := app.DumpOpenAPI(tmpFile); err != nil {
		t.Fatalf("DumpOpenAPI failed: %v", err)
	}

	if !app.IsWarmedUp() {
		t.Errorf("expected app to be warmed up after DumpOpenAPI")
	}
}

func TestContext_BindQueryAndForm(t *testing.T) {
	type FilterReq struct {
		Search string `json:"search"`
		Page   string `json:"page"`
	}

	app := nimbus.New(nimbus.WithMode(nimbus.ModeTest))
	app.Router.Get("/search", func(c *nhttp.Context) error {
		var req FilterReq
		if err := c.BindQuery(&req); err != nil {
			return err
		}
		c.Text(stdhttp.StatusOK, "search="+req.Search+",page="+req.Page)
		return nil
	})

	client := ntest.New(app)
	res := client.Get("/search?search=golang&page=2")
	res.AssertOK(t)
	res.AssertContains(t, "search=golang,page=2")
}

func TestContext_SSE(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeTest))
	app.Router.Get("/events", func(c *nhttp.Context) error {
		return c.SSEStream(func(w *nhttp.SSEWriter) error {
			if err := w.Event("message", "hello world"); err != nil {
				return err
			}
			return w.Event("ping", map[string]string{"status": "alive"})
		})
	})

	client := ntest.New(app)
	res := client.Get("/events")
	res.AssertOK(t)
	res.AssertHeader(t, "Content-Type", "text/event-stream")
	res.AssertContains(t, "event: message\ndata: hello world\n\n")
	res.AssertContains(t, "event: ping\ndata: {\"status\":\"alive\"}\n\n")
}

func TestContext_TextHTMLValidationErrors(t *testing.T) {
	app := nimbus.New(nimbus.WithMode(nimbus.ModeTest))
	app.Router.Get("/html", func(c *nhttp.Context) error {
		c.HTML(stdhttp.StatusOK, "<h1>Hello</h1>")
		return nil
	})
	app.Router.Post("/validate", func(c *nhttp.Context) error {
		return c.ValidationErrors(map[string][]string{
			"email": {"email is required"},
		})
	})

	client := ntest.New(app)
	resHTML := client.Get("/html")
	resHTML.AssertOK(t)
	resHTML.AssertHeader(t, "Content-Type", "text/html; charset=utf-8")
	resHTML.AssertContains(t, "<h1>Hello</h1>")

	resVal := client.PostJSON("/validate", nil)
	resVal.AssertStatus(t, stdhttp.StatusUnprocessableEntity)
	resVal.AssertJSONPath(t, "message", "The given data was invalid.")
}
