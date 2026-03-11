package container

import (
	"fmt"
	"reflect"
	"sync"
)

// Constructor is a function that builds a value (lazy). Return T or (T, error).
type Constructor any

// Container is a simple IoC container: bind name → constructor, resolve with Make (AdonisJS/Laravel style).
type Container struct {
	mu          sync.RWMutex
	bindings    map[string]Constructor
	singletons  map[string]any
	singletonOk map[string]bool
}

// New returns a new container.
func New() *Container {
	return &Container{
		bindings:    make(map[string]Constructor),
		singletons:  make(map[string]any),
		singletonOk: make(map[string]bool),
	}
}

// Bind registers a constructor for name. Each Make(name) calls the constructor.
func (c *Container) Bind(name string, constructor Constructor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = constructor
	delete(c.singletons, name)
	delete(c.singletonOk, name)
}

// Singleton registers a constructor that is invoked once; subsequent Make returns the same instance.
func (c *Container) Singleton(name string, constructor Constructor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = constructor
	c.singletons[name] = nil
	c.singletonOk[name] = false
}

// Make resolves name to a value by calling the registered constructor.
func (c *Container) Make(name string) (any, error) {
	c.mu.RLock()
	f, ok := c.bindings[name]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("container: no binding for %q", name)
	}

	// Singleton: return cached if already built
	c.mu.RLock()
	if v, done := c.singletons[name]; done && c.singletonOk[name] {
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after lock
	if v, done := c.singletons[name]; done && c.singletonOk[name] {
		return v, nil
	}

	v, err := c.invoke(f)
	if err != nil {
		return nil, err
	}
	if _, isSingleton := c.singletons[name]; isSingleton {
		c.singletons[name] = v
		c.singletonOk[name] = true
	}
	return v, nil
}

func (c *Container) invoke(f Constructor) (any, error) {
	rf := reflect.ValueOf(f)
	if rf.Kind() != reflect.Func {
		return nil, fmt.Errorf("container: binding must be a function")
	}
	t := rf.Type()
	if t.NumIn() > 0 {
		// Could support resolving args from container here
		return nil, fmt.Errorf("container: constructor with arguments not yet supported")
	}
	out := rf.Call(nil)
	if len(out) == 0 {
		return nil, fmt.Errorf("container: constructor must return (value) or (value, error)")
	}
	var err error
	if len(out) == 2 && !out[1].IsNil() {
		err = out[1].Interface().(error)
	}
	if err != nil {
		return nil, err
	}
	return out[0].Interface(), nil
}

// MustMake is like Make but panics on error.
func (c *Container) MustMake(name string) any {
	v, err := c.Make(name)
	if err != nil {
		panic(err)
	}
	return v
}

// Instance registers a pre-built value directly (no constructor).
// Subsequent Make calls return this exact value.
//
//	c.Instance("stripe", stripeClient)
func (c *Container) Instance(name string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = func() any { return value }
	c.singletons[name] = value
	c.singletonOk[name] = true
}

// Has returns true if a binding exists for the given name.
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.bindings[name]
	return ok
}
