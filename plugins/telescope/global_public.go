package telescope

import "time"

// RecordLog writes a log entry to Telescope when the plugin is active.
func RecordLog(level, message string, context map[string]any) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	s.Record(&Entry{
		Type: EntryLog,
		Content: map[string]any{
			"level":   level,
			"message": message,
			"context": context,
		},
	})
}

// RecordEventFromDispatch records a generic event (call from events.AfterDispatch hook).
func RecordEventFromDispatch(name string, payload any) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	s.Record(&Entry{
		Type: EntryEvent,
		Content: map[string]any{
			"event":   name,
			"payload": payload,
		},
	})
}

// RecordViewRender records template render timing (wired from view.OnRendered).
func RecordViewRender(name string, duration time.Duration, data any) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	n := 0
	if m, ok := data.(map[string]any); ok {
		n = len(m)
	}
	s.Record(&Entry{
		Type: EntryView,
		Content: map[string]any{
			"name":        name,
			"duration_ms": duration.Milliseconds(),
			"data_keys":   n,
		},
	})
}

// RecordScheduleRun records a scheduled task execution.
func RecordScheduleRun(name, expression, status string, duration time.Duration, output string) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	content := map[string]any{
		"task":        name,
		"expression":  expression,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	}
	if output != "" {
		content["output"] = truncate(output, 1000)
	}
	s.Record(&Entry{Type: EntrySchedule, Content: content})
}
