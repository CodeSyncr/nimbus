package telescope

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/queue"
)

func registerErrorHook() {
	errors.RegisterExceptionRecorder(func(class, message, file string, line int, trace string) {
		recordException(class, message, file, line, trace)
	})
}

func recordException(class, message, file string, line int, trace string) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}

	// Improve parity with Laravel Telescope: panics should show up as "panic",
	// and file/line should be populated when we have a stack trace.
	if class == "AppError" && strings.HasPrefix(message, "panic:") {
		class = "panic"
	}
	if file == "" && line == 0 && trace != "" {
		if f, ln := firstFrameFromStack(trace); f != "" && ln > 0 {
			file, line = f, ln
		}
	}

	s.Record(&Entry{
		Type: EntryException,
		Content: map[string]any{
			"class":   class,
			"message": message,
			"file":    file,
			"line":    line,
			"trace":   trace,
		},
	})
}

func firstFrameFromStack(trace string) (file string, line int) {
	// debug.Stack() format includes lines like:
	//   /path/to/file.go:123 +0x...
	lines := strings.Split(trace, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.Contains(l, ".go:") {
			continue
		}
		// Skip runtime/internal frames as best effort.
		if strings.Contains(l, "/runtime/") || strings.Contains(l, "/reflect/") || strings.Contains(l, "runtime.goexit") {
			continue
		}
		colon := strings.LastIndex(l, ":")
		if colon < 0 || colon+1 >= len(l) {
			continue
		}
		rest := l[colon+1:]
		// rest is like "123 +0x..." or "123"
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		ln, err := strconv.Atoi(fields[0])
		if err != nil || ln <= 0 {
			continue
		}
		return l[:colon], ln
	}
	return "", 0
}

// queueObserver forwards job lifecycle to Telescope entries.
type queueObserver struct{}

func (queueObserver) JobDispatched(payload *queue.JobPayload) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil || payload == nil {
		return
	}
	var decoded any
	if len(payload.Payload) > 0 {
		var m map[string]any
		if err := json.Unmarshal(payload.Payload, &m); err == nil {
			decoded = m
		}
	}
	s.Record(&Entry{
		Type: EntryJob,
		Tags: []string{"dispatched"},
		Content: map[string]any{
			"name":    payload.JobName,
			"queue":   payload.Queue,
			"status":  "dispatched",
			"id":      payload.ID,
			"payload": decoded,
			"attempt": payload.Attempts,
		},
	})
}

func (queueObserver) JobProcessed(payload *queue.JobPayload, err error) {
	// Legacy fallback when V2 isn't available.
	(queueObserver{}).JobProcessedV2(payload, 0, err)
}

func (queueObserver) JobProcessedV2(payload *queue.JobPayload, duration time.Duration, err error) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil || payload == nil {
		return
	}
	status := "processed"
	if err != nil {
		status = "failed"
	}
	var decoded any
	if len(payload.Payload) > 0 {
		var m map[string]any
		if e := json.Unmarshal(payload.Payload, &m); e == nil {
			decoded = m
		}
	}
	s.Record(&Entry{
		Type: EntryJob,
		Tags: []string{status},
		Content: map[string]any{
			"name":        payload.JobName,
			"queue":       payload.Queue,
			"status":      status,
			"id":          payload.ID,
			"error":       errString(err),
			"attempt":     payload.Attempts,
			"duration_ms": duration.Milliseconds(),
			"payload":     decoded,
		},
	})
}

func (queueObserver) JobRetried(payload *queue.JobPayload, nextDelay time.Duration) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil || payload == nil {
		return
	}
	s.Record(&Entry{
		Type: EntryJob,
		Tags: []string{"retried"},
		Content: map[string]any{
			"name":          payload.JobName,
			"queue":         payload.Queue,
			"status":        "retried",
			"id":            payload.ID,
			"attempt":       payload.Attempts,
			"next_delay_ms": nextDelay.Milliseconds(),
		},
	})
}

func (queueObserver) JobsReclaimed(queueName string, count int) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil || count <= 0 {
		return
	}
	s.Record(&Entry{
		Type: EntryJob,
		Tags: []string{"reclaimed"},
		Content: map[string]any{
			"queue":  queueName,
			"status": "reclaimed",
			"count":  count,
		},
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// RegisterQueueObserver subscribes to global queue events (call from Plugin.Register).
func RegisterQueueObserver() {
	queue.AddObserver(queueObserver{})
}
