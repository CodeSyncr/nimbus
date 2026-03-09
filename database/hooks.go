package database

import (
	"gorm.io/gorm"
)

// HookFunc is a function that runs at a model lifecycle point.
type HookFunc func(db *gorm.DB)

// RegisterHooks registers GORM callbacks for a model (Lucid-style hooks).
// Use with db.Callback() or pass the model's table name.
//
// Example:
//
//	database.RegisterHooks(db, "users", database.Hooks{
//	    BeforeCreate: func(db *gorm.DB) { /* hash password */ },
//	    AfterCreate:  func(db *gorm.DB) { /* send welcome email */ },
//	})
type Hooks struct {
	BeforeCreate HookFunc
	AfterCreate  HookFunc
	BeforeUpdate HookFunc
	AfterUpdate  HookFunc
	BeforeSave   HookFunc
	AfterSave    HookFunc
	BeforeDelete HookFunc
	AfterDelete  HookFunc
}

// RegisterHooks registers the given hooks for the model.
// Hooks are registered globally per callback name; for model-specific hooks,
// use GORM's Scopes or register in your model's init.
func RegisterHooks(db *gorm.DB, name string, h Hooks) {
	if h.BeforeCreate != nil {
		db.Callback().Create().Before("gorm:before_create").Register("nimbus:before_create_"+name, h.BeforeCreate)
	}
	if h.AfterCreate != nil {
		db.Callback().Create().After("gorm:after_create").Register("nimbus:after_create_"+name, h.AfterCreate)
	}
	if h.BeforeUpdate != nil {
		db.Callback().Update().Before("gorm:before_update").Register("nimbus:before_update_"+name, h.BeforeUpdate)
	}
	if h.AfterUpdate != nil {
		db.Callback().Update().After("gorm:after_update").Register("nimbus:after_update_"+name, h.AfterUpdate)
	}
	if h.BeforeSave != nil {
		db.Callback().Create().Before("gorm:before_create").Register("nimbus:before_save_create_"+name, h.BeforeSave)
		db.Callback().Update().Before("gorm:before_update").Register("nimbus:before_save_update_"+name, h.BeforeSave)
	}
	if h.AfterSave != nil {
		db.Callback().Create().After("gorm:after_create").Register("nimbus:after_save_create_"+name, h.AfterSave)
		db.Callback().Update().After("gorm:after_update").Register("nimbus:after_save_update_"+name, h.AfterSave)
	}
	if h.BeforeDelete != nil {
		db.Callback().Delete().Before("gorm:before_delete").Register("nimbus:before_delete_"+name, h.BeforeDelete)
	}
	if h.AfterDelete != nil {
		db.Callback().Delete().After("gorm:after_delete").Register("nimbus:after_delete_"+name, h.AfterDelete)
	}
}
