package lucid

import "gorm.io/gorm"

// DB is Nimbus's first-party ORM database handle.
type DB = gorm.DB

// Config configures ORM behavior.
type Config = gorm.Config

// Dialector selects the underlying SQL dialect.
type Dialector = gorm.Dialector

// Option is an ORM open option.
type Option = gorm.Option

// DeletedAt is the soft-delete field type.
type DeletedAt = gorm.DeletedAt

// Session configures a DB session (scopes, dry run, new connection, etc.).
type Session = gorm.Session

// Statement is the active query/model statement (callbacks, schema, SQL).
type Statement = gorm.Statement

// ErrRecordNotFound is returned when no row matches a query.
var ErrRecordNotFound = gorm.ErrRecordNotFound

// ErrInvalidData can be returned from hooks to roll back a transaction.
var ErrInvalidData = gorm.ErrInvalidData

// Open initializes a Lucid database connection.
func Open(dialector Dialector, opts ...Option) (*DB, error) {
	return gorm.Open(dialector, opts...)
}
