package database

import (
	"github.com/CodeSyncr/nimbus/lucid"
)

// Transaction runs fn inside a transaction. On success it commits; on error it rolls back.
// Lucid-style managed transaction.
func Transaction(db *lucid.DB, fn func(tx *lucid.DB) error) error {
	return db.Transaction(fn)
}

// TransactionWithDB runs fn inside a transaction using the global DB.
func TransactionWithDB(fn func(tx *lucid.DB) error) error {
	return Get().Transaction(fn)
}
