package telescope

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/notification"
	"github.com/CodeSyncr/nimbus/queue"
)

func recordEntry(typ EntryType, content map[string]any) {
	recordEntryWithTags(typ, content, nil)
}

func recordEntryWithTags(typ EntryType, content map[string]any, tags []string) {
	storeMu.RLock()
	s := globalStore
	storeMu.RUnlock()
	if s == nil {
		return
	}
	s.Record(&Entry{Type: typ, Content: content, Tags: tags})
}

// ── Integrations (cache, mail, gate, notification) ─────────────────

var integrationsOnce sync.Once

func (p *Plugin) registerIntegrations() {
	integrationsOnce.Do(func() {
		wrapGlobalCache()
		registerTelescopeRedisHook()
		auth.DefaultGate().After(func(ctx context.Context, user auth.User, ability string, result bool) {
			uid := ""
			if user != nil {
				uid = fmt.Sprint(user.GetID())
			}
			tags := []string{"gate"}
			if result {
				tags = append(tags, "allowed")
			} else {
				tags = append(tags, "denied")
			}
			recordEntryWithTags(EntryGate, map[string]any{
				"ability": ability,
				"result":  result,
				"user_id": uid,
			}, tags)
		})
		notification.AfterSend(func(n notification.Notification, err error) {
			name := fmt.Sprintf("%T", n)
			channels := notificationChannels(n)
			content := map[string]any{
				"notification": name,
				"channels":     channels,
				"success":      err == nil,
			}
			tags := []string{"notification"}
			if err != nil {
				content["error"] = err.Error()
				tags = append(tags, "error")
			}
			recordEntryWithTags(EntryNotification, content, tags)
		})
		queue.AfterBatch(func(_ context.Context, b *queue.Batch) {
			if b == nil {
				return
			}
			total := int(b.TotalJobs())
			failed := int(b.FailedJobs())
			pending := int(b.PendingJobs())
			progress := 0.0
			if total > 0 {
				progress = float64(total-pending-failed) / float64(total)
			}
			tags := []string{"batch"}
			if b.HasFailures() {
				tags = append(tags, "error")
			}
			recordEntryWithTags(EntryBatch, map[string]any{
				"id":           b.ID,
				"name":         "batch",
				"total_jobs":   total,
				"pending_jobs": pending,
				"failed_jobs":  failed,
				"progress":     fmt.Sprintf("%.0f%%", progress*100),
				"has_failures": b.HasFailures(),
			}, tags)
		})
		cli.AfterCommand(func(ctx *cli.Context, d time.Duration, err error) {
			if ctx == nil || ctx.Cmd == nil {
				return
			}
			exitCode := 0
			if err != nil {
				exitCode = 1
			}
			content := map[string]any{
				"command":     ctx.Cmd.CommandPath(),
				"args":        ctx.Args,
				"exit_code":   exitCode,
				"duration_ms": d.Milliseconds(),
			}
			if err != nil {
				content["error"] = err.Error()
			}
			recordEntry(EntryCommand, content)
		})
	})
}

func notificationChannels(n notification.Notification) []string {
	var ch []string
	if n != nil && n.ToMail() != nil {
		ch = append(ch, "mail")
	}
	if n != nil {
		if c, _ := n.ToBroadcast(); c != "" {
			ch = append(ch, "broadcast:"+c)
		}
	}
	return ch
}

var (
	cacheWrapOnce sync.Once
	mailWrapOnce  sync.Once
)

func wrapGlobalCache() {
	cacheWrapOnce.Do(func() {
		inner := cache.GetGlobal()
		if inner == nil {
			return
		}
		if _, ok := inner.(*telescopeCacheWrapper); ok {
			return
		}
		cache.SetGlobal(&telescopeCacheWrapper{inner: inner})
	})
}

type telescopeCacheWrapper struct {
	inner cache.Store
}

func (w *telescopeCacheWrapper) Set(key string, value any, ttl time.Duration) error {
	start := time.Now()
	err := w.inner.Set(key, value, ttl)
	tags := []string{"cache", "set"}
	if err != nil {
		tags = append(tags, "error")
	}
	recordEntryWithTags(EntryCache, map[string]any{
		"operation":   "set",
		"key":         key,
		"ttl_ms":      ttl.Milliseconds(),
		"duration_ms": time.Since(start).Milliseconds(),
	}, tags)
	return err
}

func (w *telescopeCacheWrapper) Get(key string) (any, bool) {
	start := time.Now()
	v, ok := w.inner.Get(key)
	tags := []string{"cache", "get"}
	if ok {
		tags = append(tags, "hit")
	} else {
		tags = append(tags, "miss")
	}
	recordEntryWithTags(EntryCache, map[string]any{
		"operation":   "get",
		"key":         key,
		"hit":         ok,
		"duration_ms": time.Since(start).Milliseconds(),
	}, tags)
	return v, ok
}

func (w *telescopeCacheWrapper) Delete(key string) error {
	start := time.Now()
	err := w.inner.Delete(key)
	tags := []string{"cache", "delete"}
	if err != nil {
		tags = append(tags, "error")
	}
	recordEntryWithTags(EntryCache, map[string]any{
		"operation":   "delete",
		"key":         key,
		"duration_ms": time.Since(start).Milliseconds(),
	}, tags)
	return err
}

func (w *telescopeCacheWrapper) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	start := time.Now()
	v, err := w.inner.Remember(key, ttl, fn)
	m := map[string]any{
		"operation":   "remember",
		"key":         key,
		"ttl_ms":      ttl.Milliseconds(),
		"duration_ms": time.Since(start).Milliseconds(),
	}
	tags := []string{"cache", "remember"}
	if err != nil {
		m["remember_error"] = err.Error()
		tags = append(tags, "error")
	}
	recordEntryWithTags(EntryCache, m, tags)
	return v, err
}

func wrapMailDriverForTelescope() {
	mailWrapOnce.Do(func() {
		if mail.Default == nil {
			return
		}
		if _, ok := mail.Default.(*telescopeMailWrapper); ok {
			return
		}
		mail.Default = &telescopeMailWrapper{inner: mail.Default}
	})
}

type telescopeMailWrapper struct {
	inner mail.Driver
}

func (d *telescopeMailWrapper) Send(m *mail.Message) error {
	err := d.inner.Send(m)
	to := ""
	subj := ""
	preview := ""
	if m != nil {
		if len(m.To) > 0 {
			to = strings.Join(m.To, ", ")
		}
		subj = m.Subject
		if m.Body != "" {
			preview = m.Body
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
		}
	}
	tags := []string{"mail"}
	if err != nil {
		tags = append(tags, "error")
	}
	recordEntryWithTags(EntryMail, map[string]any{
		"to":           to,
		"subject":      subj,
		"success":      err == nil,
		"body_preview": preview,
		"mailer":       fmt.Sprintf("%T", d.inner),
		"error":        errString(err),
	}, tags)
	return err
}
