package telescope

import (
	"encoding/json"
	"sync"
)

var (
	globalStore *Store
	storeMu     sync.RWMutex
)

func setGlobalStore(s *Store) {
	storeMu.Lock()
	defer storeMu.Unlock()
	globalStore = s
}

// Dump records a variable dump to Telescope. Call from your handlers or middleware.
// Example: telescope.Dump("user", user)
func Dump(label string, v any) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	var val string
	if b, err := json.MarshalIndent(v, "", "  "); err == nil {
		val = string(b)
	} else {
		val = "<?>"
	}
	s.Record(&Entry{
		Type: EntryDump,
		Content: map[string]any{
			"label": label,
			"value": val,
		},
	})
}
