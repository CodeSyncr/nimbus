package telescope

import (
	"context"
	"time"

	"github.com/CodeSyncr/nimbus/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// telescopeLogger wraps GORM's default logger and records queries to the store.
type telescopeLogger struct {
	logger.Interface
	store *Store
}

func (l *telescopeLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rows int64), err error) {
	sql, rows := fc()
	if sql != "" {
		l.store.Record(&Entry{
			Type: EntryQuery,
			Content: map[string]any{
				"sql":         sql,
				"duration_ms": time.Since(begin).Milliseconds(),
				"rows":        rows,
			},
		})
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

// RegisterQueryWatcher attaches a custom logger and model callbacks to the global database.
func (p *Plugin) RegisterQueryWatcher() {
	db := database.Get()
	if db == nil {
		return
	}
	inner := db.Logger
	if inner == nil {
		inner = logger.Default
	}
	db.Logger = &telescopeLogger{Interface: inner, store: p.store}
	p.registerModelWatcher(db)
}

func (p *Plugin) registerModelWatcher(db *gorm.DB) {
	db.Callback().Create().After("gorm:create").Register("telescope:model_create", func(d *gorm.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "created",
				"model": tableName(d.Statement),
			},
		})
	})
	db.Callback().Update().After("gorm:update").Register("telescope:model_update", func(d *gorm.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "updated",
				"model": tableName(d.Statement),
			},
		})
	})
	db.Callback().Delete().After("gorm:delete").Register("telescope:model_delete", func(d *gorm.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "deleted",
				"model": tableName(d.Statement),
			},
		})
	})
}

func tableName(stmt *gorm.Statement) string {
	if stmt.Schema != nil {
		return stmt.Schema.Table
	}
	return "unknown"
}
