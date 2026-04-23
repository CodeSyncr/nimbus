package cli

import (
	"sync"
	"time"
)

var (
	afterCommandMu  sync.RWMutex
	afterCommandFns []func(ctx *Context, duration time.Duration, err error)
)

// AfterCommand registers a hook invoked after any Nimbus CLI command runs.
// This is used by observability tooling (e.g. Telescope) without coupling
// the cli package to those plugins.
func AfterCommand(fn func(ctx *Context, duration time.Duration, err error)) {
	if fn == nil {
		return
	}
	afterCommandMu.Lock()
	defer afterCommandMu.Unlock()
	afterCommandFns = append(afterCommandFns, fn)
}

func runAfterCommandHooks(ctx *Context, duration time.Duration, err error) {
	afterCommandMu.RLock()
	fns := make([]func(ctx *Context, duration time.Duration, err error), len(afterCommandFns))
	copy(fns, afterCommandFns)
	afterCommandMu.RUnlock()

	for _, fn := range fns {
		fn(ctx, duration, err)
	}
}
