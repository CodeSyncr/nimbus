package database

import (
	"reflect"
	"strings"
)

// RelationKind describes the type of relationship between models.
type RelationKind string

const (
	RelationBelongsTo  RelationKind = "belongsTo"
	RelationHasMany    RelationKind = "hasMany"
	RelationHasOne     RelationKind = "hasOne"
	RelationManyToMany RelationKind = "manyToMany"
)

// Relation describes a single relation discovered from struct tags or
// inferred from Go struct conventions.
//
// Explicit tags (override conventions):
//

//	User     User    `nimbus:"belongsTo,foreignKey:AuthorID"`
//	Posts    []Post   `nimbus:"hasMany,foreignKey:AuthorID"`
//	Teams   []Team    `nimbus:"manyToMany,pivotTable:user_teams"`
//
// Auto-inferred (no tag needed):
//
//	ClientID uint              // FK field
//	Client   *Client           // belongsTo — inferred because ClientID exists
//	Posts    []Post             // hasMany — inferred because Post has UserID
//	Profile  *Profile          // hasOne — inferred (single struct, no FK on this model)
//	Teams    []Team             // manyToMany — inferred because Team has no UserID
//
// Tags are only needed to override conventions (custom FK names, pivot tables, etc.).
// The shorter "relation" tag key also works:
//
//	Teams []Team `nimbus:"manyToMany,pivotTable:custom_pivot"`
type Relation struct {
	Kind           RelationKind
	FieldName      string       // Go struct field name (e.g. "User", "Posts", "Teams")
	TargetType     reflect.Type // Element type (e.g. User, Post, Team — never a pointer/slice)
	ForeignKey     string       // FK column (belongsTo: on this model; hasMany/hasOne: on related)
	LocalKey       string       // PK on this model (default "ID")
	OwnerKey       string       // PK on related model (default "ID")
	PivotTable     string       // manyToMany: pivot table name (auto-generated if empty)
	JoinForeignKey string       // manyToMany: FK in pivot pointing to this model
	JoinReferences string       // manyToMany: FK in pivot pointing to related model
	TagRaw         string
}

// ParseRelations inspects a model and returns all relations — both explicitly
// tagged and auto-inferred from Go struct conventions.
//
// Inference rules (when no nimbus/relation tag is present):
//
//   - belongsTo: exported struct/pointer field where FieldNameID exists on the
//     same model (e.g. Client *Client + ClientID uint → belongsTo).
//   - hasMany:   exported slice-of-structs field (e.g. Posts []Post).
//   - hasOne:    exported struct/pointer field where FieldNameID does NOT exist
//     on this model (e.g. Profile *Profile without ProfileID).
//   - manyToMany: CANNOT be inferred — must use explicit
//     nimbus:"manyToMany" or relation:"manyToMany" tag.
//
// Explicitly tagged fields always take precedence over inference.
func ParseRelations(model any) []Relation {
	t := reflect.TypeOf(model)
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	// Collect all exported field names for FK detection.
	fieldNames := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			fieldNames[t.Field(i).Name] = true
		}
	}

	var rels []Relation
	seen := make(map[string]bool) // track field names already added

	// ── Pass 1: explicitly tagged fields ───────────────────────────────
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		tag := sf.Tag.Get("nimbus")
		if tag == "" {
			tag = sf.Tag.Get("relation")
		}
		if tag == "" {
			continue
		}

		r := Relation{
			FieldName: sf.Name,
			TagRaw:    tag,
		}

		parts := strings.Split(tag, ",")
		if len(parts) == 0 {
			continue
		}

		// First part: kind
		head := strings.TrimSpace(parts[0])
		if head == "" {
			continue
		}
		headParts := strings.SplitN(head, ":", 2)
		kind := strings.TrimSpace(headParts[0])
		switch kind {
		case "belongsTo":
			r.Kind = RelationBelongsTo
		case "hasMany":
			r.Kind = RelationHasMany
		case "hasOne":
			r.Kind = RelationHasOne
		case "manyToMany":
			r.Kind = RelationManyToMany
		default:
			continue
		}

		// Remaining parts: key:value pairs.
		for _, p := range parts[1:] {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			kv := strings.SplitN(p, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			switch k {
			case "foreignKey":
				r.ForeignKey = v
			case "localKey":
				r.LocalKey = v
			case "ownerKey":
				r.OwnerKey = v
			case "pivotTable":
				r.PivotTable = v
			case "joinForeignKey":
				r.JoinForeignKey = v
			case "joinReferences":
				r.JoinReferences = v
			}
		}

		// Derive target type from field type.
		ft := sf.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			ft = ft.Elem()
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
		}
		if ft.Kind() == reflect.Struct {
			r.TargetType = ft
		}

		seen[sf.Name] = true
		rels = append(rels, r)
	}

	// ── Pass 2: auto-infer untagged relation fields ────────────────────
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() || seen[sf.Name] || sf.Anonymous {
			continue
		}

		ft := sf.Type
		isPtr := ft.Kind() == reflect.Ptr
		isSlice := ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array

		if isPtr {
			ft = ft.Elem()
		}

		elemType := ft
		if isSlice {
			elemType = ft.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
		}

		// Only consider struct element types (skip time.Time, gorm.DeletedAt, etc.)
		if elemType.Kind() != reflect.Struct || isSkippedType(elemType) {
			continue
		}

		if isSlice {
			// Slice of structs: check if the related model has OwnerTypeID field.
			// e.g. User.Posts []Post → does Post have UserID? Yes → hasMany. No → manyToMany.
			expectedFK := t.Name() + "ID"
			if relatedHasField(elemType, expectedFK) {
				rels = append(rels, Relation{
					Kind:       RelationHasMany,
					FieldName:  sf.Name,
					TargetType: elemType,
				})
			} else {
				rels = append(rels, Relation{
					Kind:       RelationManyToMany,
					FieldName:  sf.Name,
					TargetType: elemType,
				})
			}
		} else if isPtr || ft.Kind() == reflect.Struct {
			// Single struct/pointer: check if FieldNameID exists → belongsTo, else hasOne
			if fieldNames[sf.Name+"ID"] {
				rels = append(rels, Relation{
					Kind:       RelationBelongsTo,
					FieldName:  sf.Name,
					TargetType: elemType,
				})
			} else {
				rels = append(rels, Relation{
					Kind:       RelationHasOne,
					FieldName:  sf.Name,
					TargetType: elemType,
				})
			}
		}
	}

	return rels
}

// relatedHasField checks if a struct type has an exported field with the given name.
func relatedHasField(t reflect.Type, name string) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	_, ok := t.FieldByName(name)
	return ok
}

// isSkippedType returns true for well-known struct types that are NOT model relations.
func isSkippedType(t reflect.Type) bool {
	full := t.PkgPath() + "." + t.Name()
	switch full {
	case "time.Time",
		"gorm.io/gorm.DeletedAt",
		"database/sql.NullTime",
		"database/sql.NullString",
		"database/sql.NullInt64",
		"database/sql.NullFloat64",
		"database/sql.NullBool":
		return true
	}
	// Skip the nimbus Model / BaseModel embedded struct itself
	if t.Name() == "Model" && strings.HasSuffix(t.PkgPath(), "/database") {
		return true
	}
	return false
}
