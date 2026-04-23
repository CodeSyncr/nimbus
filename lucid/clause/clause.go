// Package clause re-exports GORM clause builders under the Nimbus lucid module.
package clause

import gclause "gorm.io/gorm/clause"

// Locking adds SELECT ... FOR UPDATE style locking to queries.
type Locking = gclause.Locking
