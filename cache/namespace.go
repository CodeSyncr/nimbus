package cache

import "time"

// NamespaceStore is a Store scoped to a namespace, with Clear to remove all entries in that namespace.
type NamespaceStore interface {
	Store
	Clear() error
}

// Namespace returns a Store that prefixes all keys with the given namespace.
// Use for grouping related cache entries and clearing them together.
//
// Example:
//
//	usersCache := cache.Namespace("users")
//	usersCache.Set("42", user, 10*time.Minute)  // stores under "users:42"
//	usersCache.Clear()                          // clears all "users:*" (Memory & Redis)
func Namespace(prefix string) NamespaceStore {
	if prefix != "" && prefix[len(prefix)-1] != ':' {
		prefix += ":"
	}
	return &namespacedStore{
		store:  GetGlobal(),
		prefix: prefix,
	}
}

type namespacedStore struct {
	store  Store
	prefix string
}

func (n *namespacedStore) key(k string) string {
	return n.prefix + k
}

func (n *namespacedStore) Set(key string, value any, ttl time.Duration) error {
	s := n.store
	if s == nil {
		s = Default
	}
	return s.Set(n.key(key), value, ttl)
}

func (n *namespacedStore) Get(key string) (any, bool) {
	s := n.store
	if s == nil {
		s = Default
	}
	return s.Get(n.key(key))
}

func (n *namespacedStore) Delete(key string) error {
	s := n.store
	if s == nil {
		s = Default
	}
	return s.Delete(n.key(key))
}

func (n *namespacedStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	s := n.store
	if s == nil {
		s = Default
	}
	return s.Remember(n.key(key), ttl, fn)
}

// Clear removes all keys in this namespace. Supported by MemoryStore and RedisStore.
func (n *namespacedStore) Clear() error {
	return InvalidatePrefix(n.prefix)
}

var _ NamespaceStore = (*namespacedStore)(nil)
