// Package scheduler is DEPRECATED. Use github.com/CodeSyncr/nimbus/schedule instead.
//
// This package exists only for backward compatibility and delegates entirely
// to the schedule package which provides named tasks, panic recovery, and
// daily-at scheduling.
package scheduler

import (
	"github.com/CodeSyncr/nimbus/schedule"
)

// Scheduler is a DEPRECATED alias for schedule.Scheduler.
// Use schedule.New() directly.
type Scheduler = schedule.Scheduler

// New returns a new Scheduler (delegates to schedule.New).
//
// Deprecated: Use schedule.New() instead.
func New() *Scheduler {
	return schedule.New()
}
