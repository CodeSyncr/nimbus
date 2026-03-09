package database

import (
	"time"

	"github.com/CodeSyncr/nimbus/cache"
	"gorm.io/gorm"
)

// CachedFind runs the query and caches the result. Use for read-heavy, rarely-changing data.
// Key should be unique per query (e.g. "users:list", "user:1").
//
// Example:
//
//	var users []User
//	err := database.CachedFind(db.Model(&User{}).Where("active = ?", true), "users:active", 10*time.Minute, &users)
func CachedFind(db *gorm.DB, key string, ttl time.Duration, dest any) error {
	_, err := cache.Remember(key, ttl, func() (any, error) {
		clone := db.Session(&gorm.Session{})
		err := clone.Find(dest).Error
		return dest, err
	})
	return err
}
