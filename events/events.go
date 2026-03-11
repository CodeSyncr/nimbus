// Package events provides a lightweight pub/sub event dispatcher for Nimbus.
//
// Register listeners and fire events:
//
//	events.Listen("user.created", func(payload any) error {
//	    u := payload.(*models.User)
//	    // send welcome email…
//	    return nil
//	})
//	events.Dispatch("user.created", user)
package events

import (
	"log"
	"sync"
)

// Event is a type for event names (e.g. "user.created").
type Event = string

// ── Framework lifecycle events ──────────────────────────────────
// These are dispatched automatically by the app at each stage.
// Listen for them in plugins or userland code:
//
//	app.Events.Listen(events.AppBooted, func(payload any) error { … })
const (
	// Boot sequence
	ProviderRegister = "provider:register" // payload: nil — all providers registered
	PluginRegister   = "plugin:register"   // payload: nil — all plugins registered + bindings
	ProviderBoot     = "provider:boot"     // payload: nil — all providers booted
	PluginBoot       = "plugin:boot"       // payload: nil — all plugins booted

	// App lifecycle
	AppBooted   = "app:booted"   // payload: nil — boot complete, capabilities applied
	AppStarted  = "app:started"  // payload: string (port) — server listening
	AppShutdown = "app:shutdown" // payload: os.Signal — graceful shutdown started

	// Route registration
	RouteRegistered      = "route:registered"      // payload: nil — plugin routes mounted
	MiddlewareRegistered = "middleware:registered" // payload: nil — plugin middleware merged
	// Database events
	DatabaseQuery  = "db:query"
	DatabaseInsert = "db:insert"
	DatabaseUpdate = "db:update"
	DatabaseDelete = "db:delete"
)

// Listener handles an event. Return an error to signal failure.
type Listener func(payload any) error

// Dispatcher is the event bus.
type Dispatcher struct {
	mu        sync.RWMutex
	listeners map[string][]Listener
}

// New returns a new Dispatcher.
func New() *Dispatcher {
	return &Dispatcher{listeners: make(map[string][]Listener)}
}

// Listen registers a listener for the event.
func (d *Dispatcher) Listen(event string, fn Listener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners[event] = append(d.listeners[event], fn)
}

// Dispatch fires the event synchronously. All listeners run in order.
// Returns the first error encountered; remaining listeners still run.
func (d *Dispatcher) Dispatch(event string, payload any) error {
	d.mu.RLock()
	fns := d.listeners[event]
	d.mu.RUnlock()

	var firstErr error
	for _, fn := range fns {
		if err := fn(payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DispatchAsync fires the event asynchronously. Each listener runs in
// its own goroutine; errors are logged.
func (d *Dispatcher) DispatchAsync(event string, payload any) {
	d.mu.RLock()
	fns := d.listeners[event]
	d.mu.RUnlock()

	for _, fn := range fns {
		go func(f Listener) {
			if err := f(payload); err != nil {
				log.Printf("[events] async listener error for %q: %v", event, err)
			}
		}(fn)
	}
}

// Has returns true if the event has at least one listener.
func (d *Dispatcher) Has(event string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.listeners[event]) > 0
}

// Clear removes all listeners for the given events, or all if none specified.
func (d *Dispatcher) Clear(events ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(events) == 0 {
		d.listeners = make(map[string][]Listener)
		return
	}
	for _, e := range events {
		delete(d.listeners, e)
	}
}

// ListenerCount returns the number of listeners for the given event.
func (d *Dispatcher) ListenerCount(event string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.listeners[event])
}

// ── Package-level helpers (use the global dispatcher) ───────────

// Default is the application-wide event dispatcher.
var Default = New()

// Listen is a shortcut for Default.Listen.
func Listen(event string, fn Listener) { Default.Listen(event, fn) }

// Dispatch is a shortcut for Default.Dispatch.
func Dispatch(event string, payload any) error { return Default.Dispatch(event, payload) }

// DispatchAsync is a shortcut for Default.DispatchAsync.
func DispatchAsync(event string, payload any) { Default.DispatchAsync(event, payload) }
