package database

import (
	"gorm.io/gorm"
)

// Preload loads associations (Lucid eager loading).
// Use with Query or Model: db.Preload("User").Preload("Comments").Find(&posts)
//
// Relations on your models should be expressed with Nimbus tags instead of
// GORM-specific tags so your models stay framework-agnostic. For example:
//
//	type Post struct {
//	    database.Model
//	    UserID   uint
//	    User     User     `nimbus:"belongsTo:User,foreignKey:UserID"`
//	    Comments []Comment `nimbus:"hasMany:Comment,foreignKey:PostID"`
//	}
//
// Nimbus can then map these relations to the underlying ORM (GORM today)
// while keeping your model types decoupled from GORM tags.
//
//	User hasMany Posts
//	Post belongsTo User
//	User manyToMany Teams (pivot: user_teams)
func Preload(db *gorm.DB, name string, args ...any) *gorm.DB {
	if len(args) > 0 {
		return db.Preload(name, args...)
	}
	return db.Preload(name)
}
