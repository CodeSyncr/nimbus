package nimbus_test

import (
	"os"
	"testing"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	"gorm.io/gorm"
)

type User struct {
	ID   uint   `gorm:"primaryKey"`
	Name string
}

func TestTransaction(t *testing.T) {
	// Setup in-memory SQLite
	db, err := database.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	nimbus.SetDB(db)

	// Migrate the schema
	db.AutoMigrate(&User{})

	t.Run("Commit on Success", func(t *testing.T) {
		err := nimbus.Transaction(func(tx *nimbus.DB) error {
			return tx.Create(&User{Name: "Alice"}).Error
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var user User
		if err := db.First(&user, "name = ?", "Alice").Error; err != nil {
			t.Errorf("expected user Alice to be created, got error: %v", err)
		}
	})

	t.Run("Rollback on Error", func(t *testing.T) {
		err := nimbus.Transaction(func(tx *nimbus.DB) error {
			tx.Create(&User{Name: "Bob"})
			return gorm.ErrInvalidData // force rollback
		})
		if err == nil {
			t.Error("expected error, got nil")
		}

		var user User
		if err := db.First(&user, "name = ?", "Bob").Error; err == nil {
			t.Error("expected user Bob NOT to be created")
		}
	})

	t.Run("Manual Transaction", func(t *testing.T) {
		tx := nimbus.Begin()
		if tx == nil {
			t.Fatal("expected tx, got nil")
		}
		
		tx.Create(&User{Name: "Charlie"})
		tx.Commit()

		var user User
		if err := db.First(&user, "name = ?", "Charlie").Error; err != nil {
			t.Errorf("expected user Charlie to be created, got error: %v", err)
		}
	})

	// Cleanup
	os.Remove("nimbus.db") // Just in case it was created
}
