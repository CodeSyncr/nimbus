package database

import (
	"time"

	"github.com/CodeSyncr/nimbus/events"
	"gorm.io/gorm"
)

// Event constants for database operations.
const (
	EventQuery  = "db:query"
	EventInsert = "db:insert"
	EventUpdate = "db:update"
	EventDelete = "db:delete"
)

// QueryPayload is dispatched for every executed query.
type QueryPayload struct {
	SQL          string
	Vars         []any
	RowsAffected int64
	Duration     time.Duration
	Error        error
}

// eventPlugin is a GORM plugin that hooks into lifecycle callbacks to dispatch events.
type eventPlugin struct{}

func (e *eventPlugin) Name() string {
	return "nimbus:events"
}

func (e *eventPlugin) Initialize(db *gorm.DB) error {
	// Register before/after callbacks for the 4 main operations

	// Query
	_ = db.Callback().Query().Before("gorm:query").Register("nimbus:before_query", e.before)
	_ = db.Callback().Query().After("gorm:query").Register("nimbus:after_query", e.after(EventQuery))

	// Create (Insert)
	_ = db.Callback().Create().Before("gorm:create").Register("nimbus:before_create", e.before)
	_ = db.Callback().Create().After("gorm:create").Register("nimbus:after_create", e.after(EventInsert))

	// Update
	_ = db.Callback().Update().Before("gorm:update").Register("nimbus:before_update", e.before)
	_ = db.Callback().Update().After("gorm:update").Register("nimbus:after_update", e.after(EventUpdate))

	// Delete
	_ = db.Callback().Delete().Before("gorm:delete").Register("nimbus:before_delete", e.before)
	_ = db.Callback().Delete().After("gorm:delete").Register("nimbus:after_delete", e.after(EventDelete))

	return nil
}

// before simply injects a start timer into the DB instance context
func (e *eventPlugin) before(db *gorm.DB) {
	db.InstanceSet("nimbus:start_time", time.Now())
}

// after dispatches the event using the recorded start time
func (e *eventPlugin) after(eventName string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		// Only dispatch if there is someone listening (optimization)
		if !events.Default.Has(eventName) {
			return
		}

		var duration time.Duration
		if start, ok := db.InstanceGet("nimbus:start_time"); ok {
			duration = time.Since(start.(time.Time))
		}

		// Dialector Explain generates the exact SQL string
		sql := db.Dialector.Explain(db.Statement.SQL.String(), db.Statement.Vars...)

		payload := QueryPayload{
			SQL:          sql,
			Vars:         db.Statement.Vars,
			RowsAffected: db.Statement.RowsAffected,
			Duration:     duration,
			Error:        db.Error,
		}

		// Fire asynchronously to avoid blocking the DB operation
		events.DispatchAsync(eventName, payload)
	}
}
