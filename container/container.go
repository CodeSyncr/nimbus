package container

import (
	"fmt"
	"reflect"
	"sync"
)

// Constructor is a function that builds a value (lazy). Return T or (T, error).
type Constructor any

// Container is a simple IoC container: bind name → constructor, resolve with Make (AdonisJS/Laravel style).
// Supports auto-wiring: constructors whose parameters are interface or pointer
// types are resolved automatically from the container's type bindings.
type Container struct {
	mu          sync.RWMutex
	bindings    map[string]Constructor
	singletons  map[string]any
	singletonOk map[string]bool
	typeMap     map[reflect.Type]string // maps Go type → binding name for auto-wiring
}

// New returns a new container.
func New() *Container {
	return &Container{
		bindings:    make(map[string]Constructor),
		singletons:  make(map[string]any),
		singletonOk: make(map[string]bool),
		typeMap:     make(map[reflect.Type]string),
	}
}

// Bind registers a constructor for name. Each Make(name) calls the constructor.
func (c *Container) Bind(name string, constructor Constructor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = constructor
	delete(c.singletons, name)
	delete(c.singletonOk, name)
	c.registerType(name, constructor)
}

// Singleton registers a constructor that is invoked once; subsequent Make returns the same instance.
func (c *Container) Singleton(name string, constructor Constructor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = constructor
	c.singletons[name] = nil
	c.singletonOk[name] = false
	c.registerType(name, constructor)
}

// registerType extracts the return type of a constructor and adds it to the type map.
// Must be called while holding c.mu write lock.
func (c *Container) registerType(name string, constructor Constructor) {
	rf := reflect.ValueOf(constructor)
	if rf.Kind() != reflect.Func {
		return
	}
	t := rf.Type()
	if t.NumOut() > 0 {
		outType := t.Out(0)
		c.typeMap[outType] = name
	}
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

	// Auto-wire: resolve constructor parameters from the container by type.
	args := make([]reflect.Value, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		paramType := t.In(i)
		resolved, err := c.resolveType(paramType)
		if err != nil {
			return nil, fmt.Errorf("container: cannot auto-wire parameter %d (%s): %w", i, paramType, err)
		}
		args[i] = reflect.ValueOf(resolved)
	}

	out := rf.Call(args)
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

// resolveType looks up a binding whose return type matches the requested type.
// It checks both exact type matches and interface satisfaction.
func (c *Container) resolveType(paramType reflect.Type) (any, error) {
	// Check direct type match.
	if name, ok := c.typeMap[paramType]; ok {
		// Temporarily release write lock, acquire read lock to call Make.
		c.mu.Unlock()
		val, err := c.Make(name)
		c.mu.Lock()
		return val, err
	}

	// Check if any binding's return type implements the requested interface.
	if paramType.Kind() == reflect.Interface {
		for _, constructor := range c.bindings {
			rf := reflect.ValueOf(constructor)
			if rf.Kind() != reflect.Func {
				continue
			}
			ct := rf.Type()
			if ct.NumOut() > 0 && ct.Out(0).Implements(paramType) {
				// Find the name for this binding.
				for name, b := range c.bindings {
					if reflect.ValueOf(b).Pointer() == rf.Pointer() {
						c.mu.Unlock()
						val, err := c.Make(name)
						c.mu.Lock()
						return val, err
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("no binding registered for type %s", paramType)
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
	// Register the concrete type for auto-wiring.
	if value != nil {
		c.typeMap[reflect.TypeOf(value)] = name
	}
}

// Has returns true if a binding exists for the given name.
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.bindings[name]
	return ok
}
