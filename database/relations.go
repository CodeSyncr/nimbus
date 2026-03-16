package database

import (
	"gorm.io/gorm"
)

// Preload loads associations via GORM's built-in eager loading.
// For belongsTo / hasOne / hasMany, GORM detects relationships automatically
// from Go conventions (e.g. UserID + User field).
//
// For manyToMany relations, use database.Load() instead — it handles pivot
// tables using nimbus tags without requiring gorm:"many2many" tags.
//
// Model definition (convention-based, no tags needed):
//
//	type Post struct {
//	    database.Model
//	    UserID   uint
//	    User     *User
//	    Comments []Comment
//	}
//
//	type User struct {
//	    database.Model
//	    Posts   []Post
//	    Teams   []Team
//	}
//
// Loading relations:
//
//	// GORM Preload (belongsTo / hasMany / hasOne)
//	db.Preload("User").Preload("Comments").Find(&posts)
//
//	// Nimbus Load (all relation types, including manyToMany)
//	database.Load(db, &user, "Posts", "Teams")
//
//	// Nimbus query builder for relations
//	database.Related(db, &user, "Posts").Where("published = ?", true).Find(&posts)
//
//	// ManyToMany pivot operations
//	database.Attach(db, &user, "Teams", &team1, &team2)
//	database.Detach(db, &user, "Teams", &team1)
//	database.Sync(db, &user, "Teams", &team1, &team3)
func Preload(db *gorm.DB, name string, args ...any) *gorm.DB {
	if len(args) > 0 {
		return db.Preload(name, args...)
	}
	return db.Preload(name)
}
