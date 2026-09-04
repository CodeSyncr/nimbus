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
		{
			Name: "2026_07_15_000003_create_cashier_purchases",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Purchase{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&Purchase{}) },
		},
		{
			Name: "2026_07_15_000004_create_cashier_refunds",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Refund{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&Refund{}) },
		},
		{
			Name: "2026_07_15_000005_extend_cashier_subscriptions",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Subscription{}) },
			Down: func(db *lucid.DB) error { return nil },
		},
		{
			Name: "2026_09_02_000006_subscription_entitlement_context",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&Subscription{}) },
			Down: func(db *lucid.DB) error { return nil },
		},
	}
}
