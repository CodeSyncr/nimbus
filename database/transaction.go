package database

import (
	"gorm.io/gorm"
)

// Transaction runs fn inside a transaction. On success it commits; on error it rolls back.
// Lucid-style managed transaction.
func Transaction(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.Transaction(fn)
}

// TransactionWithDB runs fn inside a transaction using the global DB.
func TransactionWithDB(fn func(tx *gorm.DB) error) error {
	return Get().Transaction(fn)
}
