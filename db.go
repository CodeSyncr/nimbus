package nimbus

import (
	"context"
	"fmt"

	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/database/nosql"
	"gorm.io/gorm"
)

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

// Connection returns a named database connection from the connection manager.
// If the named connection exists, it returns it. Otherwise, falls back
// to the default connection.
//
//	db := nimbus.Connection("analytics")
//	db.Find(&events)
func Connection(name string) *DB {
	if conn := database.Connection(name); conn != nil {
		return conn
	}
	return db
}

// Transaction runs fn inside a database transaction.
// It automatically commits if no error is returned, or rolls back if an error occurs.
func Transaction(fn func(tx *DB) error) error {
	if db == nil {
		return fmt.Errorf("nimbus: database not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

// Begin starts a manual database transaction.
// The caller is responsible for calling Commit() or Rollback() on the returned *DB.
func Begin() *DB {
	if db == nil {
		return nil
	}
	return db.Begin()
}

// ═══════════════════════════════════════════════════════════════════
// NoSQL — Application-Level Access
// ═══════════════════════════════════════════════════════════════════
//
// NoSQL provides application-level access to a document database,
// mirroring the *nimbus.DB pattern for SQL. Controllers and services
// can depend on *nimbus.NoSQL instead of importing the nosql package.
//
// Usage in controllers:
//
//   type OrderController struct {
//       Store *nimbus.NoSQL
//   }
//
//   func (oc *OrderController) Index(ctx *http.Context) error {
//       var orders []Order
//       oc.Store.Collection("orders").Find(ctx.Request.Context(), nosql.Filter{"active": true}, &orders)
//       return ctx.JSON(200, orders)
//   }

// NoSQL is Nimbus's high-level NoSQL handle.
// It wraps nosql.Driver so controllers depend on *nimbus.NoSQL
// instead of the lower-level nosql package directly.
type NoSQL struct {
	nosql.Driver
}

// noSQLStore is the global NoSQL handle set by the framework at boot.
var noSQLStore *NoSQL

// SetNoSQL is called by the framework (or hosting app) to make the NoSQL
// connection globally available as *nimbus.NoSQL.
func SetNoSQL(driver nosql.Driver) {
	if driver == nil {
		noSQLStore = nil
		return
	}
	noSQLStore = &NoSQL{Driver: driver}
}

// GetNoSQL returns the global NoSQL handle, or nil if not initialised.
func GetNoSQL() *NoSQL {
	return noSQLStore
}

// NoSQLConnection returns a named NoSQL connection wrapped in *nimbus.NoSQL.
// Falls back to the default NoSQL connection if the name isn't registered.
//
//	store := nimbus.NoSQLConnection("mongo")
//	store.Collection("users").FindOne(ctx, filter, &user)
func NoSQLConnection(name string) *NoSQL {
	if conn := nosql.Connection(name); conn != nil {
		return &NoSQL{Driver: conn}
	}
	return noSQLStore
}

// NoSQLCollection is a convenience that returns a Collection handle
// from the default NoSQL connection.
//
//	coll := nimbus.NoSQLCollection("users")
//	coll.InsertOne(ctx, user)
func NoSQLCollection(name string) nosql.Collection {
	if noSQLStore == nil {
		return nil
	}
	return noSQLStore.Collection(name)
}

// CloseNoSQL closes the default NoSQL connection.
func CloseNoSQL(ctx context.Context) error {
	if noSQLStore == nil {
		return nil
	}
	return noSQLStore.Close(ctx)
}
