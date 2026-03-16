package telescope

import (
	"fmt"
	"time"
)

// ── Watcher recording functions ─────────────────────────────────
// These functions are called by framework subsystems to record entries.
// They can also be called from application code.

// RecordJob records a queued job execution.
func (p *Plugin) RecordJob(name, queue, status string, duration time.Duration, payload map[string]any, err error) {
	content := map[string]any{
		"name":        name,
		"queue":       queue,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	}
	if payload != nil {
		content["payload"] = payload
	}
	if err != nil {
		content["error"] = err.Error()
		content["status"] = "failed"
	}
	p.store.Record(&Entry{
		Type:    EntryJob,
		Content: content,
	})
}

// RecordCache records a cache operation (hit, miss, set, delete, flush).
func (p *Plugin) RecordCache(op, key string, hit bool, duration time.Duration) {
	content := map[string]any{
		"operation":   op,
		"key":         key,
		"hit":         hit,
		"duration_ms": duration.Milliseconds(),
	}
	p.store.Record(&Entry{
		Type:    EntryCache,
		Content: content,
	})
}

// RecordMail records a sent email.
func (p *Plugin) RecordMail(to, subject, mailer string, success bool, body string) {
	content := map[string]any{
		"to":      to,
		"subject": subject,
		"mailer":  mailer,
		"success": success,
	}
	if body != "" {
		content["body_preview"] = truncate(body, 500)
	}
	p.store.Record(&Entry{
		Type:    EntryMail,
		Content: content,
	})
}

// RecordNotification records a notification dispatch.
func (p *Plugin) RecordNotification(channel, notifiable, notification string, data map[string]any) {
	content := map[string]any{
		"channel":      channel,
		"notifiable":   notifiable,
		"notification": notification,
	}
	if data != nil {
		content["data"] = data
	}
	p.store.Record(&Entry{
		Type:    EntryNotification,
		Content: content,
	})
}

// RecordEvent records an event dispatch.
func (p *Plugin) RecordEvent(name string, listeners []string, payload map[string]any) {
	content := map[string]any{
		"event":     name,
		"listeners": listeners,
	}
	if payload != nil {
		content["payload"] = payload
	}
	p.store.Record(&Entry{
		Type:    EntryEvent,
		Content: content,
	})
}

// RecordCommand records a CLI command execution.
func (p *Plugin) RecordCommand(name string, args []string, exitCode int, duration time.Duration) {
	p.store.Record(&Entry{
		Type: EntryCommand,
		Content: map[string]any{
			"command":     name,
			"args":        args,
			"exit_code":   exitCode,
			"duration_ms": duration.Milliseconds(),
		},
	})
}

// RecordSchedule records a scheduled task execution.
func (p *Plugin) RecordSchedule(name, expression, status string, duration time.Duration, output string) {
	content := map[string]any{
		"task":        name,
		"expression":  expression,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	}
	if output != "" {
		content["output"] = truncate(output, 1000)
	}
	p.store.Record(&Entry{
		Type:    EntrySchedule,
		Content: content,
	})
}

// RecordGate records an authorization gate check.
func (p *Plugin) RecordGate(ability string, result bool, userID string, arguments map[string]any) {
	content := map[string]any{
		"ability": ability,
		"result":  result,
		"user_id": userID,
	}
	if arguments != nil {
		content["arguments"] = arguments
	}
	p.store.Record(&Entry{
		Type:    EntryGate,
		Content: content,
	})
}

// RecordHTTPClient records an outgoing HTTP request.
func (p *Plugin) RecordHTTPClient(method, url string, statusCode int, duration time.Duration, requestHeaders, responseHeaders map[string]string) {
	content := map[string]any{
		"method":      method,
		"url":         url,
		"status":      statusCode,
		"duration_ms": duration.Milliseconds(),
	}
	if requestHeaders != nil {
		content["request_headers"] = requestHeaders
	}
	if responseHeaders != nil {
		content["response_headers"] = responseHeaders
	}
	p.store.Record(&Entry{
		Type:    EntryHTTPClient,
		Content: content,
	})
}

// RecordRedis records a Redis command.
func (p *Plugin) RecordRedis(command string, duration time.Duration, connection string) {
	p.store.Record(&Entry{
		Type: EntryRedis,
		Content: map[string]any{
			"command":     command,
			"duration_ms": duration.Milliseconds(),
			"connection":  connection,
		},
	})
}

// RecordLog records an application log message.
func (p *Plugin) RecordLog(level, message string, context map[string]any) {
	content := map[string]any{
		"level":   level,
		"message": message,
	}
	if context != nil {
		content["context"] = context
	}
	p.store.Record(&Entry{
		Type:    EntryLog,
		Content: content,
	})
}

// RecordBatch records a job batch operation.
func (p *Plugin) RecordBatch(id, name string, totalJobs, pendingJobs, failedJobs int, progress float64) {
	p.store.Record(&Entry{
		Type: EntryBatch,
		Content: map[string]any{
			"id":           id,
			"name":         name,
			"total_jobs":   totalJobs,
			"pending_jobs": pendingJobs,
			"failed_jobs":  failedJobs,
			"progress":     fmt.Sprintf("%.0f%%", progress*100),
		},
	})
}

// RecordView records a template render.
func (p *Plugin) RecordView(name string, duration time.Duration, data map[string]any) {
	content := map[string]any{
		"name":        name,
		"duration_ms": duration.Milliseconds(),
	}
	if data != nil {
		content["composers"] = len(data)
	}
	p.store.Record(&Entry{
		Type:    EntryView,
		Content: content,
	})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
