package database

import (
	"fmt"
	"reflect"

	"github.com/jinzhu/inflection"
	"gorm.io/gorm"
)

// Attach adds related models to a manyToMany relation via the pivot table.
// AdonisJS equivalent: await user.related('teams').attach([teamId1, teamId2])
//
//	database.Attach(db, &user, "Teams", &team1, &team2)
func Attach(db *gorm.DB, model any, relationName string, related ...any) error {
	rel := findRelationByName(model, relationName)
	if rel == nil {
		return fmt.Errorf("database: relation %q not found on %T", relationName, model)
	}
	if rel.Kind != RelationManyToMany {
		return fmt.Errorf("database: Attach only works with manyToMany relations, got %s", rel.Kind)
	}

	pivot, joinFK, joinRef := pivotConfig(model, *rel)
	localID := getFieldValue(model, localKeyFor(*rel))

	for _, r := range related {
		relatedID := getFieldValue(r, ownerKeyFor(*rel))
		if relatedID == nil || isZeroValue(relatedID) {
			continue
		}

		row := map[string]any{
			joinFK:  localID,
			joinRef: relatedID,
		}

		// Skip if already exists.
		var count int64
		db.Table(pivot).Where(row).Count(&count)
		if count > 0 {
			continue
		}

		if err := db.Table(pivot).Create(row).Error; err != nil {
			return fmt.Errorf("database: attaching %s: %w", relationName, err)
		}
	}
	return nil
}

// Detach removes related models from a manyToMany relation via the pivot table.
// AdonisJS equivalent: await user.related('teams').detach([teamId1])
//
//	database.Detach(db, &user, "Teams", &team1)
func Detach(db *gorm.DB, model any, relationName string, related ...any) error {
	rel := findRelationByName(model, relationName)
	if rel == nil {
		return fmt.Errorf("database: relation %q not found on %T", relationName, model)
	}
	if rel.Kind != RelationManyToMany {
		return fmt.Errorf("database: Detach only works with manyToMany relations, got %s", rel.Kind)
	}

	pivot, joinFK, joinRef := pivotConfig(model, *rel)
	localID := getFieldValue(model, localKeyFor(*rel))

	for _, r := range related {
		relatedID := getFieldValue(r, ownerKeyFor(*rel))
		if relatedID == nil || isZeroValue(relatedID) {
			continue
		}

		if err := db.Table(pivot).
			Where(fmt.Sprintf("%s = ? AND %s = ?", joinFK, joinRef), localID, relatedID).
			Delete(map[string]any{}).Error; err != nil {
			return fmt.Errorf("database: detaching %s: %w", relationName, err)
		}
	}
	return nil
}

// DetachAll removes all related models from a manyToMany pivot table.
//
//	database.DetachAll(db, &user, "Teams")
func DetachAll(db *gorm.DB, model any, relationName string) error {
	rel := findRelationByName(model, relationName)
	if rel == nil {
		return fmt.Errorf("database: relation %q not found on %T", relationName, model)
	}
	if rel.Kind != RelationManyToMany {
		return fmt.Errorf("database: DetachAll only works with manyToMany relations, got %s", rel.Kind)
	}

	pivot, joinFK, _ := pivotConfig(model, *rel)
	localID := getFieldValue(model, localKeyFor(*rel))

	return db.Table(pivot).
		Where(fmt.Sprintf("%s = ?", joinFK), localID).
		Delete(map[string]any{}).Error
}

// Sync replaces all existing pivot records with the given related models.
// AdonisJS equivalent: await user.related('teams').sync([teamId1, teamId3])
//
//	database.Sync(db, &user, "Teams", &team1, &team3)
func Sync(db *gorm.DB, model any, relationName string, related ...any) error {
	if err := DetachAll(db, model, relationName); err != nil {
		return err
	}
	if len(related) == 0 {
		return nil
	}
	return Attach(db, model, relationName, related...)
}

// MigratePivots scans nimbus tags on the given models and creates any
// missing manyToMany pivot tables. Call after database.Boot().
//
//	database.MigratePivots(db, &User{}, &Post{}, &Tag{})
func MigratePivots(db *gorm.DB, models ...any) error {
	seen := make(map[string]bool)
	for _, model := range models {
		for _, rel := range ParseRelations(model) {
			if rel.Kind != RelationManyToMany {
				continue
			}
			pivot, joinFK, joinRef := pivotConfig(model, rel)
			if seen[pivot] {
				continue
			}
			seen[pivot] = true

			if db.Migrator().HasTable(pivot) {
				continue
			}

			sql := fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (%s integer NOT NULL, %s integer NOT NULL, PRIMARY KEY (%s, %s))",
				pivot, joinFK, joinRef, joinFK, joinRef,
			)
			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("database: creating pivot table %s: %w", pivot, err)
			}
		}
	}
	return nil
}

// HasRelated checks if a manyToMany relation has a specific related model attached.
//
//	attached, err := database.HasRelated(db, &user, "Teams", &team)
func HasRelated(db *gorm.DB, model any, relationName string, related any) (bool, error) {
	rel := findRelationByName(model, relationName)
	if rel == nil {
		return false, fmt.Errorf("database: relation %q not found on %T", relationName, model)
	}
	if rel.Kind != RelationManyToMany {
		return false, fmt.Errorf("database: HasRelated only works with manyToMany relations")
	}

	pivot, joinFK, joinRef := pivotConfig(model, *rel)
	localID := getFieldValue(model, localKeyFor(*rel))
	relatedID := getFieldValue(related, ownerKeyFor(*rel))

	var count int64
	err := db.Table(pivot).
		Where(fmt.Sprintf("%s = ? AND %s = ?", joinFK, joinRef), localID, relatedID).
		Count(&count).Error
	return count > 0, err
}

// ── helpers ─────────────────────────────────────────────────────

func localKeyFor(rel Relation) string {
	if rel.LocalKey != "" {
		return rel.LocalKey
	}
	return "ID"
}

func ownerKeyFor(rel Relation) string {
	if rel.OwnerKey != "" {
		return rel.OwnerKey
	}
	return "ID"
}

// relatedTypeTable returns the conventional table name for a related type name.
// Used for debug/logging only; prefer tableNameForType for actual queries.
func relatedTypeTable(typeName string) string {
	return inflection.Plural(toSnake(typeName))
}

// pivotConfigFromType is a convenience to call pivotConfig using a reflect.Type
// for the owner instead of a model instance.
func pivotConfigFromType(ownerType reflect.Type, rel Relation) (string, string, string) {
	if ownerType.Kind() == reflect.Ptr {
		ownerType = ownerType.Elem()
	}
	ownerName := toSnake(ownerType.Name())
	relatedName := toSnake(rel.TargetType.Name())

	pivot := rel.PivotTable
	if pivot == "" {
		pivot = generatePivotName(ownerName, relatedName)
	}

	joinFK := rel.JoinForeignKey
	if joinFK == "" {
		joinFK = ownerName + "_id"
	}

	joinRef := rel.JoinReferences
	if joinRef == "" {
		joinRef = relatedName + "_id"
	}

	return pivot, joinFK, joinRef
}
