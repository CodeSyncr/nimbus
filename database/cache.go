package database

import (
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/internal/cachehook"
	"github.com/CodeSyncr/nimbus/lucid"
)

// CachedFind runs the query and caches the result. Use for read-heavy, rarely-changing data.
// Key should be unique per query (e.g. "users:list", "user:1").
//
// Example:
//
//	var users []User
//	err := database.CachedFind(db.Model(&User{}).Where("active = ?", true), "users:active", 10*time.Minute, &users)
func CachedFind(db *lucid.DB, key string, ttl time.Duration, dest any) error {
	remember := cachehook.Remember
	if remember == nil {
		remember = memoryRemember
	}
	v, err := remember(key, ttl, func() (any, error) {
		clone := db.Session(&lucid.Session{})
		err := clone.Find(dest).Error
		return dest, err
	})
	if err != nil || v == nil || v == dest {
		return err
	}
	// A cache hit returns the value stored by an earlier call: copy it into
	// dest, directly when the types match, through JSON when a distributed
	// store handed back decoded data.
	dv, cv := reflect.ValueOf(dest), reflect.ValueOf(v)
	if dv.Kind() == reflect.Pointer && cv.Type() == dv.Type() {
		dv.Elem().Set(cv.Elem())
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// memoryRemember is the in-process cache CachedFind uses when the app has
// not linked the cache package, the same behaviour as cache.Default.
var memory sync.Map // key -> memoryEntry

type memoryEntry struct {
	value   any
	expires time.Time
}

func memoryRemember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if e, ok := memory.Load(key); ok {
		if entry := e.(memoryEntry); ttl <= 0 || time.Now().Before(entry.expires) {
			return entry.value, nil
		}
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	memory.Store(key, memoryEntry{value: v, expires: time.Now().Add(ttl)})
	return v, nil
}
