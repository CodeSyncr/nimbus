package database

import (
	"gorm.io/gorm"
)

// Preload loads associations (Lucid eager loading).
// Use with Query or Model: db.Preload("User").Preload("Comments").Find(&posts)
//
// GORM relations: define on your model with tags:
//
//	type Post struct {
//	    database.Model
//	    UserID   uint
//	    User     User   `gorm:"foreignKey:UserID"`
//	    Comments []Comment `gorm:"foreignKey:PostID"`
//	}
//
// Or use the relation helpers:
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
