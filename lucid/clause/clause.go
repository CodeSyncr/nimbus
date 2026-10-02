// Package clause re-exports GORM clause builders under the Nimbus lucid module.
package clause

import gclause "gorm.io/gorm/clause"

// Locking adds SELECT ... FOR UPDATE style locking to queries.
type Locking = gclause.Locking

// OnConflict adds INSERT ... ON CONFLICT (upsert) handling.
type OnConflict = gclause.OnConflict

// Column names a column in a clause.
type Column = gclause.Column

// Expr is a raw SQL expression with bind variables.
type Expr = gclause.Expr

// AssignmentColumns updates the named columns from the inserted row on conflict.
func AssignmentColumns(values []string) gclause.Set { return gclause.AssignmentColumns(values) }

// Assignments builds a SET list from a column -> value map.
func Assignments(values map[string]interface{}) gclause.Set { return gclause.Assignments(values) }
