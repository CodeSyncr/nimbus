package telescope

import (
	"context"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	goredis "github.com/redis/go-redis/v9"
)

type telescopeRedisHook struct {
	connection string
}

func (h telescopeRedisHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}

func (h telescopeRedisHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		tags := []string{}
		if err != nil {
			tags = append(tags, "error")
		}
		content := map[string]any{
			"command":     strings.TrimSpace(cmd.String()),
			"duration_ms": time.Since(start).Milliseconds(),
			"connection":  h.connection,
			"success":     err == nil,
		}
		if err != nil {
			content["error"] = err.Error()
		}
		recordEntryWithTags(EntryRedis, content, tags)
		return err
	}
}

func (h telescopeRedisHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		// Summarize pipeline as one entry (Laravel Telescope does similar aggregation)
		var b strings.Builder
		for i, c := range cmds {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(strings.TrimSpace(c.String()))
		}
		content := map[string]any{
			"command":     b.String(),
			"duration_ms": time.Since(start).Milliseconds(),
			"connection":  h.connection,
			"pipeline":    true,
			"count":       len(cmds),
			"success":     err == nil,
		}
		tags := []string{"pipeline"}
		if err != nil {
			content["error"] = err.Error()
			tags = append(tags, "error")
		}
		recordEntryWithTags(EntryRedis, content, tags)
		return err
	}
}

func registerTelescopeRedisHook() {
	redis.RegisterHook(func(opt *redis.Options) redis.Hook {
		conn := "default"
		if opt != nil {
			if opt.Addr != "" {
				conn = opt.Addr
			}
		}
		return telescopeRedisHook{connection: conn}
	})
}
