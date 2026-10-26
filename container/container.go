package container

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Constructor builds a value lazily. Return T or (T, error).
type Constructor any

type binding struct {
	constructor Constructor
	output      reflect.Type
	singleton   bool
	mu          sync.RWMutex
	value       any
	ready       bool
	flight      singleflight.Group
}

// Container registers constructors by name and auto-wires their parameters.
// Registry locks never cover user constructors. Concurrent singleton resolution
// shares one construction per binding; independent bindings can build in parallel.
type Container struct {
	mu       sync.RWMutex
	bindings map[string]*binding
	typeMap  map[reflect.Type]string
}

func New() *Container {
	return &Container{bindings: make(map[string]*binding), typeMap: make(map[reflect.Type]string)}
}

func (c *Container) register(name string, b *binding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for typ, n := range c.typeMap {
		if n == name {
			delete(c.typeMap, typ)
		}
	}
	c.bindings[name] = b
	if b.output != nil {
		c.typeMap[b.output] = name
	}
}

func newBinding(f Constructor, singleton bool) *binding {
	b := &binding{constructor: f, singleton: singleton}
	v := reflect.ValueOf(f)
	if v.IsValid() && v.Kind() == reflect.Func && v.Type().NumOut() > 0 {
		b.output = v.Type().Out(0)
	}
	return b
}

// Bind registers a constructor called for each Make.
func (c *Container) Bind(name string, f Constructor) { c.register(name, newBinding(f, false)) }

// Singleton registers a constructor whose successful result is shared.
func (c *Container) Singleton(name string, f Constructor) { c.register(name, newBinding(f, true)) }

// Instance registers a pre-built value.
func (c *Container) Instance(name string, value any) {
	c.register(name, &binding{singleton: true, value: value, ready: true, output: reflect.TypeOf(value)})
}
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.bindings[name]
	return ok
}

func (b *binding) cached() (any, bool) { b.mu.RLock(); defer b.mu.RUnlock(); return b.value, b.ready }

type plan struct {
	binding *binding
	name    string
	args    []*plan
	types   []reflect.Type
}

// Make resolves a binding. Declared dependency cycles and ambiguous interface
// bindings return errors. Constructors may resolve independent bindings via Make;
// recursive cycles hidden inside constructor bodies must be avoided.
func (c *Container) Make(name string) (any, error) {
	c.mu.RLock()
	if b := c.bindings[name]; b != nil {
		if value, ready := b.cached(); ready {
			c.mu.RUnlock()
			return value, nil
		}
	}
	p, err := c.plan(name, make(map[string]bool))
	c.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return p.resolve()
}

// plan snapshots the dependency graph while holding only the registry read lock.
// Check the whole declared graph before waiting on any singleton, so concurrent
// callers cannot deadlock each other on a typed dependency cycle.
func (c *Container) plan(name string, path map[string]bool) (*plan, error) {
	b, ok := c.bindings[name]
	if !ok {
		return nil, fmt.Errorf("container: no binding for %q", name)
	}
	p := &plan{binding: b, name: name}
	if _, ready := b.cached(); ready {
		return p, nil
	}
	if path[name] {
		return nil, fmt.Errorf("container: dependency cycle at %q", name)
	}
	path[name] = true
	defer delete(path, name)
	f := reflect.ValueOf(b.constructor)
	if !f.IsValid() || f.Kind() != reflect.Func || f.IsNil() {
		return nil, fmt.Errorf("container: %q binding must be a non-nil function", name)
	}
	typ := f.Type()
	errorType := reflect.TypeFor[error]()
	if typ.IsVariadic() || typ.NumOut() < 1 || typ.NumOut() > 2 || (typ.NumOut() == 2 && typ.Out(1) != errorType) {
		return nil, fmt.Errorf("container: %q constructor must return T or (T, error) and cannot be variadic", name)
	}
	for i := 0; i < typ.NumIn(); i++ {
		parameter := typ.In(i)
		dependency, err := c.nameForType(parameter)
		if err != nil {
			return nil, fmt.Errorf("container: %q parameter %d: %w", name, i, err)
		}
		child, err := c.plan(dependency, path)
		if err != nil {
			return nil, err
		}
		p.args = append(p.args, child)
		p.types = append(p.types, parameter)
	}
	return p, nil
}

func (c *Container) nameForType(typ reflect.Type) (string, error) {
	if name, ok := c.typeMap[typ]; ok {
		return name, nil
	}
	if typ.Kind() == reflect.Interface {
		var names []string
		for name, b := range c.bindings {
			if b.output != nil && b.output.Implements(typ) {
				names = append(names, name)
			}
		}
		if len(names) == 1 {
			return names[0], nil
		}
		if len(names) > 1 {
			sort.Strings(names)
			return "", fmt.Errorf("ambiguous bindings for %s: %v", typ, names)
		}
	}
	return "", fmt.Errorf("no binding registered for type %s", typ)
}

func (p *plan) resolve() (any, error) {
	b := p.binding
	if value, ready := b.cached(); ready {
		return value, nil
	}
	build := func() (any, error) {
		if value, ready := b.cached(); ready {
			return value, nil
		}
		args := make([]reflect.Value, len(p.args))
		for i, child := range p.args {
			value, err := child.resolve()
			if err != nil {
				return nil, err
			}
			if value == nil {
				args[i] = reflect.Zero(p.types[i])
			} else {
				args[i] = reflect.ValueOf(value)
			}
		}
		result := reflect.ValueOf(b.constructor).Call(args)
		if len(result) == 2 && !result[1].IsNil() {
			return nil, result[1].Interface().(error)
		}
		value := result[0].Interface()
		if b.singleton {
			b.mu.Lock()
			b.value, b.ready = value, true
			b.mu.Unlock()
		}
		return value, nil
	}
	if !b.singleton {
		return build()
	}
	value, err, _ := b.flight.Do("build", build)
	return value, err
}

func (c *Container) MustMake(name string) any {
	v, err := c.Make(name)
	if err != nil {
		panic(err)
	}
	return v
}
