package nimbus

import "gorm.io/gorm"

// DB is Nimbus's high-level database handle.
// Controllers and services can depend on *nimbus.DB instead of importing gorm.
// It is defined as a type alias so that *nimbus.DB and *gorm.DB are identical
// types, but applications should reference the nimbus.DB name.
type DB = gorm.DB

// db is the global Nimbus database handle set by the framework at boot.
var db *DB

// SetDB is called by the framework (or hosting app) to make the database
// connection globally available to application code as *nimbus.DB.
func SetDB(conn *DB) {
	db = conn
}

// GetDB returns the global Nimbus database handle, or nil if not initialised.
// Application code should prefer using this over importing gorm directly.
func GetDB() *DB {
	return db
}
