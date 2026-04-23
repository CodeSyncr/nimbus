package telescope

import (
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/lucid"
)

func queryTableFromSQL(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	op := strings.ToUpper(parts[0])
	var idx int
	switch op {
	case "SELECT":
		idx = indexOfToken(parts, "FROM")
		if idx >= 0 && idx+1 < len(parts) {
			return cleanSQLIdent(parts[idx+1])
		}
	case "INSERT":
		idx = indexOfToken(parts, "INTO")
		if idx >= 0 && idx+1 < len(parts) {
			return cleanSQLIdent(parts[idx+1])
		}
	case "UPDATE":
		if len(parts) >= 2 {
			return cleanSQLIdent(parts[1])
		}
	case "DELETE":
		idx = indexOfToken(parts, "FROM")
		if idx >= 0 && idx+1 < len(parts) {
			return cleanSQLIdent(parts[idx+1])
		}
	}
	return ""
}

func indexOfToken(parts []string, want string) int {
	for i, p := range parts {
		if strings.EqualFold(p, want) {
			return i
		}
	}
	return -1
}

func cleanSQLIdent(s string) string {
	v := strings.TrimSpace(s)
	v = strings.TrimRight(v, ",;")
	v = strings.Trim(v, "`\"")
	return v
}

// RegisterQueryWatcher attaches a custom logger and model callbacks to the global database.
func (p *Plugin) RegisterQueryWatcher() {
	db := database.Get()
	if db == nil {
		return
	}

	record := func(payload any) error {
		qp, ok := payload.(database.QueryPayload)
		if !ok {
			return nil
		}
		tags := []string{}
		if qp.Error != nil {
			tags = append(tags, "error")
		}
		if qp.Duration >= 100*time.Millisecond {
			tags = append(tags, "slow")
		}
		op := "QUERY"
		sqlTrim := strings.TrimSpace(qp.SQL)
		if sqlTrim != "" {
			parts := strings.Fields(sqlTrim)
			if len(parts) > 0 && parts[0] != "" {
				op = strings.ToUpper(parts[0])
			}
			// GORM-style soft delete is typically an UPDATE that sets deleted_at.
			// For the Telescope UI, present it as DELETE to match developer intent.
			if op == "UPDATE" {
				low := strings.ToLower(sqlTrim)
				if strings.Contains(low, "deleted_at") && strings.Contains(low, " set ") {
					op = "DELETE"
				}
			}
		}
		summary := strings.TrimSpace(qp.SQL)
		if len(summary) > 140 {
			summary = summary[:140] + "…"
		}
		table := queryTableFromSQL(qp.SQL)
		content := map[string]any{
			"operation":   op,
			"table":       table,
			"summary":     summary,
			"sql":         qp.SQL,
			"bindings":    qp.Vars,
			"rows":        qp.RowsAffected,
			"duration_ms": qp.Duration.Milliseconds(),
			"connection":  qp.Connection,
		}
		if qp.Error != nil {
			content["error"] = qp.Error.Error()
		}
		p.store.Record(&Entry{Type: EntryQuery, Content: content, Tags: tags})
		return nil
	}

	// Capture ALL SQL operations, not just SELECTs.
	events.Listen(database.EventQuery, record)
	events.Listen(database.EventInsert, record)
	events.Listen(database.EventUpdate, record)
	events.Listen(database.EventDelete, record)

	p.registerModelWatcher(db)
}

func (p *Plugin) registerModelWatcher(db *lucid.DB) {
	db.Callback().Create().After("gorm:create").Register("telescope:model_create", func(d *lucid.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "created",
				"model": tableName(d.Statement),
			},
		})
	})
	db.Callback().Update().After("gorm:update").Register("telescope:model_update", func(d *lucid.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "updated",
				"model": tableName(d.Statement),
			},
		})
	})
	db.Callback().Delete().After("gorm:delete").Register("telescope:model_delete", func(d *lucid.DB) {
		p.store.Record(&Entry{
			Type: EntryModel,
			Content: map[string]any{
				"event": "deleted",
				"model": tableName(d.Statement),
			},
		})
	})
}

func tableName(stmt *lucid.Statement) string {
	if stmt.Schema != nil {
		return stmt.Schema.Table
	}
	return "unknown"
}
