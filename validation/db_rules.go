package validation

import (
	"fmt"

	"gorm.io/gorm"
)

// dbProvider is the function used to get the global *gorm.DB.
// It defaults to nil; set via SetDB or it tries to import database.DB at runtime.
var dbProvider func() *gorm.DB

// SetDB sets the database provider for unique/exists rules.
// Call this in your app bootstrap: validation.SetDB(func() *gorm.DB { return database.DB })
func SetDB(fn func() *gorm.DB) {
	dbProvider = fn
}

func getDB() *gorm.DB {
	if dbProvider != nil {
		return dbProvider()
	}
	return nil
}

// UniqueOpts configures the unique database rule.
//
// Example (AdonisJS VineJS style):
//
//	validation.String().Required().Email().Unique(validation.UniqueOpts{
//	    Table: "users",
//	    Filter: func(db *gorm.DB, value, field string) *gorm.DB {
//	        return db.Where("id != ?", currentUserID)
//	    },
//	})
type UniqueOpts struct {
	// Table is the database table to check against.
	Table string

	// Column overrides the column name (defaults to the field name).
	Column string

	// Filter adds extra conditions to the query (e.g. exclude current record).
	Filter func(db *gorm.DB, value string, field string) *gorm.DB
}

// ExistsOpts configures the exists database rule.
//
// Example:
//
//	validation.String().Required().Exists(validation.ExistsOpts{
//	    Table: "categories",
//	    Column: "slug",
//	})
type ExistsOpts struct {
	// Table is the database table to check against.
	Table string

	// Column overrides the column name (defaults to the field name).
	Column string

	// Filter adds extra conditions to the query.
	Filter func(db *gorm.DB, value string, field string) *gorm.DB
}

// checkUnique returns an error if the value already exists in the database.
func checkUnique(opts UniqueOpts, field, value string) error {
	db := getDB()
	if db == nil {
		return nil // no DB configured, skip check
	}
	col := opts.Column
	if col == "" {
		col = field
	}
	query := db.Table(opts.Table).Where(col+" = ?", value)
	if opts.Filter != nil {
		query = opts.Filter(query, value, field)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("unique check failed: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("%s has already been taken", field)
	}
	return nil
}

// checkExists returns an error if the value does NOT exist in the database.
func checkExists(opts ExistsOpts, field, value string) error {
	db := getDB()
	if db == nil {
		return nil // no DB configured, skip check
	}
	col := opts.Column
	if col == "" {
		col = field
	}
	query := db.Table(opts.Table).Where(col+" = ?", value)
	if opts.Filter != nil {
		query = opts.Filter(query, value, field)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("exists check failed: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%s does not exist", field)
	}
	return nil
}
