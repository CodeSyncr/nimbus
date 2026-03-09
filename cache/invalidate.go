package cache

// PrefixInvalidator is implemented by stores that support prefix-based invalidation.
type PrefixInvalidator interface {
	Store
	InvalidatePrefix(prefix string) error
}

// InvalidatePrefix deletes all keys with the given prefix.
// Supported by MemoryStore and RedisStore. Other stores no-op.
func InvalidatePrefix(prefix string) error {
	s := GetGlobal()
	if s == nil {
		s = Default
	}
	if pi, ok := s.(PrefixInvalidator); ok {
		return pi.InvalidatePrefix(prefix)
	}
	return nil
}
