package database

import (
	"gorm.io/gorm"
)

// ── Query Scopes ────────────────────────────────────────────────

// Scope is a reusable query modifier (like Laravel/Lucid scopes).
// Example:
//
//	var Published Scope = func(db *gorm.DB) *gorm.DB {
//	    return db.Where("published_at IS NOT NULL")
//	}
//
//	db.Scopes(Published).Find(&posts)
type Scope = func(db *gorm.DB) *gorm.DB

// WhereScope creates a simple WHERE scope.
//
//	active := database.WhereScope("active = ?", true)
//	db.Scopes(active).Find(&users)
func WhereScope(query string, args ...any) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(query, args...)
	}
}

// OrderScope creates an ORDER BY scope.
func OrderScope(column string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(column)
	}
}

// LatestScope orders by created_at DESC.
func LatestScope() Scope {
	return OrderScope("created_at DESC")
}

// OldestScope orders by created_at ASC.
func OldestScope() Scope {
	return OrderScope("created_at ASC")
}

// LimitScope limits the number of results.
func LimitScope(n int) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Limit(n)
	}
}

// WhenScope conditionally applies a scope.
//
//	db.Scopes(database.WhenScope(isAdmin, func(db *gorm.DB) *gorm.DB {
//	    return db.Where("role = ?", "admin")
//	})).Find(&users)
func WhenScope(condition bool, scope Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		if condition {
			return scope(db)
		}
		return db
	}
}

// ── Soft Delete Helpers ─────────────────────────────────────────
// These helpers work with GORM's built-in soft delete (gorm.DeletedAt).

// WithTrashed returns all records including soft-deleted ones.
func WithTrashed(db *gorm.DB) *gorm.DB {
	return db.Unscoped()
}

// OnlyTrashed returns only soft-deleted records.
func OnlyTrashed(db *gorm.DB) *gorm.DB {
	return db.Unscoped().Where("deleted_at IS NOT NULL")
}

// Restore un-deletes a soft-deleted record by setting deleted_at to NULL.
func Restore(db *gorm.DB, model any) error {
	return db.Unscoped().Model(model).Update("deleted_at", nil).Error
}

// ForceDelete permanently deletes a record (bypasses soft delete).
func ForceDelete(db *gorm.DB, model any) error {
	return db.Unscoped().Delete(model).Error
}

// IsTrashed checks if a model's DeletedAt field is set.
func IsTrashed(m *Model) bool {
	return m.DeletedAt.Valid
}

// ── Query Helpers ───────────────────────────────────────────────

// Chunk processes records in batches.
//
//	database.Chunk(db.Model(&User{}), 100, func(users []User) error {
//	    for _, u := range users {
//	        process(u)
//	    }
//	    return nil
//	})
func Chunk[T any](db *gorm.DB, size int, fn func(batch []T) error) error {
	offset := 0
	for {
		var batch []T
		result := db.Limit(size).Offset(offset).Find(&batch)
		if result.Error != nil {
			return result.Error
		}
		if len(batch) == 0 {
			break
		}
		if err := fn(batch); err != nil {
			return err
		}
		if len(batch) < size {
			break
		}
		offset += size
	}
	return nil
}

// Exists returns true if any record matches the query.
func Exists(db *gorm.DB) (bool, error) {
	var count int64
	err := db.Count(&count).Error
	return count > 0, err
}

// FirstOrCreate finds the first matching record or creates it.
func FirstOrCreate[T any](db *gorm.DB, where T, attrs T) (*T, error) {
	var result T
	err := db.Where(where).Attrs(attrs).FirstOrCreate(&result).Error
	return &result, err
}

// UpdateOrCreate finds a record by where conditions and updates it, or creates a new one.
func UpdateOrCreate[T any](db *gorm.DB, where T, update T) (*T, error) {
	var result T
	err := db.Where(where).Assign(update).FirstOrCreate(&result).Error
	return &result, err
}

// Pluck retrieves a single column from a query as a slice.
func Pluck[T any](db *gorm.DB, column string) ([]T, error) {
	var results []T
	err := db.Pluck(column, &results).Error
	return results, err
}

// CountBy counts records matching the conditions.
func CountBy(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Count(&count).Error
	return count, err
}
