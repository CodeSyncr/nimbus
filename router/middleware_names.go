package router

import (
	"reflect"
	"sync"
)

var (
	mwNameMu sync.RWMutex
	mwNames  = map[uintptr]string{}
)

// NameMiddleware associates a human-friendly name with a middleware function
// for observability tooling (e.g. Telescope). It returns the same middleware.
func NameMiddleware(name string, mw Middleware) Middleware {
	if mw == nil || name == "" {
		return mw
	}
	ptr := reflect.ValueOf(mw).Pointer()
	if ptr == 0 {
		return mw
	}
	mwNameMu.Lock()
	mwNames[ptr] = name
	mwNameMu.Unlock()
	return mw
}

func middlewareName(mw Middleware) string {
	if mw == nil {
		return ""
	}
	ptr := reflect.ValueOf(mw).Pointer()
	if ptr == 0 {
		return ""
	}
	mwNameMu.RLock()
	name := mwNames[ptr]
	mwNameMu.RUnlock()
	return name
}
