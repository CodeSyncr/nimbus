package database

import (
	"reflect"
	"strings"

	"github.com/jinzhu/inflection"
	"gorm.io/gorm"
)

// RelationProvider can be implemented by models that want to declare
// which relations should be auto-preloaded.
//
// Example:
//
//	func (Blog) Relations() []string {
//	    return []string{"User", "Comments"}
//	}
type RelationProvider interface {
	Relations() []string
}

// AutoPreload applies preloads based on either:
//   - RelationProvider.Relations(), when the model implements it, or
//   - struct tags parsed via ParseRelations as a fallback.
//
// For belongsTo / hasOne / hasMany it delegates to GORM's Preload (convention-
// based). For manyToMany it uses Nimbus's custom Load function which handles
// pivot tables automatically.
//
// Usage:
//
//	db := AutoPreload(database.Get(), &models.Blog{})
//	if err := db.Find(&blogs).Error; err != nil { ... }
func AutoPreload(db *gorm.DB, model any) *gorm.DB {
	if db == nil || model == nil {
		return db
	}

	// Prefer explicit Relations() method on the model.
	if rp, ok := model.(RelationProvider); ok {
		for _, name := range rp.Relations() {
			if name == "" {
				continue
			}
			db = Preload(db, name)
		}
		return db
	}

	// Fallback: derive relation names from nimbus/relation tags.
	for _, rel := range ParseRelations(model) {
		if rel.FieldName == "" {
			continue
		}
		// manyToMany cannot use GORM's Preload (needs nimbus tags for pivot
		// config). Use GORM callbacks to post-load via Load().
		if rel.Kind == RelationManyToMany {
			name := rel.FieldName
			db = db.Set("nimbus:autoload:"+name, true)
			continue
		}
		db = Preload(db, rel.FieldName)
	}
	return db
}

// TableNamer can be implemented by models that want to explicitly declare
// their logical table name (Laravel-style). This is separate from GORM's
// TableName() so Nimbus can remain ORM-agnostic.
//
// Example:
//
//	func (Blog) Table() string {
//	    return "blogs"
//	}
type TableNamer interface {
	Table() string
}

// FillableProvider can be implemented by models that want to declare which
// fields are mass-assignable. This metadata can be used by higher-level
// helpers (request binding, scaffolding, admin UIs, etc).
//
// Example:
//
//	func (Blog) Fillable() []string {
//	    return []string{"title", "content", "published"}
//	}
type FillableProvider interface {
	Fillable() []string
}

// TableNameFor returns the logical table name for a model. Resolution order:
//  1. TableNamer.Table()
//  2. Pluralized struct name (Blog -> blogs, UserProfile -> user_profiles)
func TableNameFor(model any) string {
	if model == nil {
		return ""
	}
	if tn, ok := model.(TableNamer); ok {
		if name := strings.TrimSpace(tn.Table()); name != "" {
			return name
		}
	}
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	snake := toSnake(t.Name())
	return inflection.Plural(snake)
}

// FillableFor returns the list of mass-assignable fields for a model.
// Resolution order:
//  1. FillableProvider.Fillable()
//  2. All exported struct fields excluding the embedded database.Model
//     and common timestamp/soft-delete fields.
func FillableFor(model any) []string {
	if model == nil {
		return nil
	}
	if fp, ok := model.(FillableProvider); ok {
		fields := fp.Fillable()
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			if strings.TrimSpace(f) != "" {
				out = append(out, f)
			}
		}
		return out
	}

	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	skip := map[string]struct{}{
		"ID":        {},
		"CreatedAt": {},
		"UpdatedAt": {},
		"DeletedAt": {},
	}

	var out []string
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		if _, ok := skip[sf.Name]; ok {
			continue
		}
		// Skip embedded database.Model and other anonymous fields.
		if sf.Anonymous {
			continue
		}
		out = append(out, sf.Name)
	}
	return out
}

// toSnake converts "UserProfile" to "user_profile". Duplicated here to avoid
// leaking cmd/nimbus internals into the database package.
func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}
