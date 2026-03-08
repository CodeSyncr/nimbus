package events

import (
	"sync"
)

// Event is a type for event names (e.g. "UserRegistered").
type Event string

// Listener is a function that handles an event. Payload is event-specific.
type Listener func(payload any)

// Bus is the event bus (plan: events.Listen, events.Dispatch).
type Bus struct {
	mu        sync.RWMutex
	listeners map[Event][]Listener
}

// NewBus returns a new event bus.
func NewBus() *Bus {
	return &Bus{listeners: make(map[Event][]Listener)}
}

// Listen registers a listener for the event (plan: events.Listen(UserRegistered, SendWelcomeEmail)).
func (b *Bus) Listen(e Event, fn Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[e] = append(b.listeners[e], fn)
}

// Dispatch fires the event to all listeners.
func (b *Bus) Dispatch(e Event, payload any) {
	b.mu.RLock()
	list := b.listeners[e]
	b.mu.RUnlock()
	for _, fn := range list {
		fn(payload)
	}
}

// Default bus for app-wide use.
var Default = NewBus()

// Listen is a shortcut for Default.Listen.
func Listen(e Event, fn Listener) { Default.Listen(e, fn) }

// Dispatch is a shortcut for Default.Dispatch.
func Dispatch(e Event, payload any) { Default.Dispatch(e, payload) }
