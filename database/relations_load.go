package database

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/jinzhu/inflection"
	"gorm.io/gorm"
)

// Load eagerly loads one or more relations onto an already-fetched model.
// This is the AdonisJS-style post-query loading:
//
//	var user User
//	db.First(&user, 1)
//	database.Load(db, &user, "Posts", "Teams")
//	// user.Posts and user.Teams are now populated
//
// For belongsTo / hasOne / hasMany, the function queries the related table
// using conventional foreign key names. For manyToMany, it queries through the
// pivot table (auto-generated or specified via pivotTable tag option).
//
// The model must be a pointer to a struct.
func Load(db *gorm.DB, model any, relations ...string) error {
	if db == nil {
		return fmt.Errorf("database: db is nil")
	}
	v := reflect.ValueOf(model)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("database: Load requires a pointer to a struct")
	}

	rels := ParseRelations(model)
	relMap := make(map[string]Relation, len(rels))
	for _, r := range rels {
		relMap[r.FieldName] = r
	}

	for _, name := range relations {
		rel, ok := relMap[name]
		if !ok {
			return fmt.Errorf("database: relation %q not found on %T", name, model)
		}
		if rel.TargetType == nil {
			return fmt.Errorf("database: relation %q has no target type", name)
		}

		var err error
		switch rel.Kind {
		case RelationBelongsTo:
			err = loadBelongsTo(db, model, rel)
		case RelationHasOne:
			err = loadHasOne(db, model, rel)
		case RelationHasMany:
			err = loadHasMany(db, model, rel)
		case RelationManyToMany:
			err = loadManyToMany(db, model, rel)
		default:
			err = fmt.Errorf("unknown relation kind %q", rel.Kind)
		}
		if err != nil {
			return fmt.Errorf("database: loading %s: %w", name, err)
		}
	}
	return nil
}

// Related returns a scoped GORM query for a relation on the given model.
// This is like AdonisJS's user.related('posts').query():
//
//	var publishedPosts []Post
//	database.Related(db, &user, "Posts").Where("published = ?", true).Find(&publishedPosts)
func Related(db *gorm.DB, model any, relationName string) *gorm.DB {
	rel := findRelationByName(model, relationName)
	if rel == nil {
		return db.Where("1 = 0") // no-op query
	}

	ownerTypeName := modelTypeName(model)

	switch rel.Kind {
	case RelationBelongsTo:
		fk := rel.ForeignKey
		if fk == "" {
			fk = rel.FieldName + "ID"
		}
		fkVal := getFieldValue(model, fk)
		key := rel.OwnerKey
		if key == "" {
			key = "id"
		}
		return db.Model(reflect.New(rel.TargetType).Interface()).Where(toSnake(key)+" = ?", fkVal)

	case RelationHasOne, RelationHasMany:
		fk := rel.ForeignKey
		if fk == "" {
			fk = ownerTypeName + "ID"
		}
		localKey := rel.LocalKey
		if localKey == "" {
			localKey = "ID"
		}
		localVal := getFieldValue(model, localKey)
		return db.Model(reflect.New(rel.TargetType).Interface()).Where(toSnake(fk)+" = ?", localVal)

	case RelationManyToMany:
		pivot, joinFK, joinRef := pivotConfig(model, *rel)
		relatedTable := tableNameForType(rel.TargetType)
		localKey := rel.LocalKey
		if localKey == "" {
			localKey = "ID"
		}
		localVal := getFieldValue(model, localKey)
		return db.Table(relatedTable).
			Joins(fmt.Sprintf(
				"INNER JOIN %s ON %s.%s = %s.id",
				pivot, pivot, joinRef, relatedTable,
			)).
			Where(fmt.Sprintf("%s.%s = ?", pivot, joinFK), localVal)
	}

	return db.Where("1 = 0")
}

// ── internal loaders ────────────────────────────────────────────

// loadBelongsTo loads a belongs-to relation.
// Convention: Post.UserID → User (FK on this model, points to related PK).
func loadBelongsTo(db *gorm.DB, model any, rel Relation) error {
	fk := rel.ForeignKey
	if fk == "" {
		fk = rel.FieldName + "ID" // User → UserID
	}

	fkVal := getFieldValue(model, fk)
	if fkVal == nil || isZeroValue(fkVal) {
		return nil // no FK set, leave relation empty
	}

	ownerKey := rel.OwnerKey
	if ownerKey == "" {
		ownerKey = "id"
	}

	target := reflect.New(rel.TargetType).Interface()
	if err := db.Where(toSnake(ownerKey)+" = ?", fkVal).First(target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	return setFieldValue(model, rel.FieldName, reflect.ValueOf(target).Elem().Interface())
}

// loadHasOne loads a has-one relation.
// Convention: User → Profile where Profile.UserID = User.ID (FK on related model).
func loadHasOne(db *gorm.DB, model any, rel Relation) error {
	ownerTypeName := modelTypeName(model)

	fk := rel.ForeignKey
	if fk == "" {
		fk = ownerTypeName + "ID" // User → UserID on Profile
	}

	localKey := rel.LocalKey
	if localKey == "" {
		localKey = "ID"
	}

	localVal := getFieldValue(model, localKey)
	if localVal == nil || isZeroValue(localVal) {
		return nil
	}

	target := reflect.New(rel.TargetType).Interface()
	if err := db.Where(toSnake(fk)+" = ?", localVal).First(target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	return setFieldValue(model, rel.FieldName, reflect.ValueOf(target).Elem().Interface())
}

// loadHasMany loads a has-many relation.
// Convention: User → Posts where Post.UserID = User.ID (FK on related model).
func loadHasMany(db *gorm.DB, model any, rel Relation) error {
	ownerTypeName := modelTypeName(model)

	fk := rel.ForeignKey
	if fk == "" {
		fk = ownerTypeName + "ID" // User → UserID on Post
	}

	localKey := rel.LocalKey
	if localKey == "" {
		localKey = "ID"
	}

	localVal := getFieldValue(model, localKey)
	if localVal == nil || isZeroValue(localVal) {
		return nil
	}

	slicePtr := reflect.New(reflect.SliceOf(rel.TargetType))
	if err := db.Where(toSnake(fk)+" = ?", localVal).Find(slicePtr.Interface()).Error; err != nil {
		return err
	}

	return setFieldValue(model, rel.FieldName, slicePtr.Elem().Interface())
}

// loadManyToMany loads a many-to-many relation via a pivot table.
// Convention: User.Teams → pivot table "team_users",
// FKs: user_id + team_id (derived from model names, alphabetically sorted).
func loadManyToMany(db *gorm.DB, model any, rel Relation) error {
	localKey := rel.LocalKey
	if localKey == "" {
		localKey = "ID"
	}
	localVal := getFieldValue(model, localKey)
	if localVal == nil || isZeroValue(localVal) {
		return nil
	}

	pivot, joinFK, joinRef := pivotConfig(model, rel)
	relatedTable := tableNameForType(rel.TargetType)

	slicePtr := reflect.New(reflect.SliceOf(rel.TargetType))
	err := db.Table(relatedTable).
		Joins(fmt.Sprintf(
			"INNER JOIN %s ON %s.%s = %s.id",
			pivot, pivot, joinRef, relatedTable,
		)).
		Where(fmt.Sprintf("%s.%s = ?", pivot, joinFK), localVal).
		Find(slicePtr.Interface()).Error
	if err != nil {
		return err
	}

	return setFieldValue(model, rel.FieldName, slicePtr.Elem().Interface())
}

// ── reflection helpers ──────────────────────────────────────────

func getFieldValue(model any, fieldName string) any {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return nil
	}
	return f.Interface()
}

func setFieldValue(model any, fieldName string, value any) error {
	v := reflect.ValueOf(model)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("model must be a pointer")
	}
	v = v.Elem()
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return fmt.Errorf("field %s not found", fieldName)
	}
	if !f.CanSet() {
		return fmt.Errorf("field %s is not settable", fieldName)
	}

	val := reflect.ValueOf(value)

	// Handle pointer vs value field types.
	// Field is *T but value is T → wrap in pointer.
	if f.Type().Kind() == reflect.Ptr && val.Type().Kind() != reflect.Ptr {
		ptr := reflect.New(val.Type())
		ptr.Elem().Set(val)
		val = ptr
	}
	// Field is T but value is *T → dereference.
	if f.Type().Kind() != reflect.Ptr && val.Type().Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if !val.Type().AssignableTo(f.Type()) {
		return fmt.Errorf("cannot assign %s to field %s (%s)", val.Type(), fieldName, f.Type())
	}
	f.Set(val)
	return nil
}

func modelTypeName(model any) string {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

func findRelationByName(model any, name string) *Relation {
	for _, r := range ParseRelations(model) {
		if r.FieldName == name {
			return &r
		}
	}
	return nil
}

func isZeroValue(v any) bool {
	return reflect.DeepEqual(v, reflect.Zero(reflect.TypeOf(v)).Interface())
}

// tableNameForType returns the database table name for a reflect.Type.
// Uses inflection to pluralize the snake_case type name.
func tableNameForType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// Check if the type implements TableNamer.
	inst := reflect.New(t).Interface()
	if tn, ok := inst.(TableNamer); ok {
		if name := tn.Table(); name != "" {
			return name
		}
	}
	return inflection.Plural(toSnake(t.Name()))
}

// pivotConfig returns (pivotTable, joinForeignKey, joinReferences) for a
// manyToMany relation, applying conventions when tag values are absent.
func pivotConfig(model any, rel Relation) (pivot, joinFK, joinRef string) {
	ownerName := toSnake(modelTypeName(model))    // e.g. "user"
	relatedName := toSnake(rel.TargetType.Name()) // e.g. "team"

	pivot = rel.PivotTable
	if pivot == "" {
		pivot = generatePivotName(ownerName, relatedName)
	}

	joinFK = rel.JoinForeignKey
	if joinFK == "" {
		joinFK = ownerName + "_id" // user_id
	}

	joinRef = rel.JoinReferences
	if joinRef == "" {
		joinRef = relatedName + "_id" // team_id
	}

	return
}

// generatePivotName creates a conventional pivot table name from two model
// names. Names are sorted alphabetically and the second is pluralized:
//
//	("user", "team") → "team_users"   (t < u)
//	("post", "tag")  → "post_tags"    (p < t)
func generatePivotName(name1, name2 string) string {
	if name1 > name2 {
		name1, name2 = name2, name1
	}
	return name1 + "_" + inflection.Plural(name2)
}
