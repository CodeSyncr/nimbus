package models

import (
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/lucid"
)

// Migrations creates the Cashier tables. Exposed via the plugin's Migrations()
// so they run alongside application migrations.
func Migrations() []database.Migration {
	return []database.Migration{
		{
			Name: "2026_07_15_000001_create_cashier_transactions",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Transaction{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&Transaction{}) },
		},
		{
			Name: "2026_07_15_000002_create_cashier_subscriptions",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Subscription{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&Subscription{}) },
		},
	}
}
