package database

import (
	"reflect"
	"strings"
)

// RelationKind describes the type of relationship between models.
type RelationKind string

const (
	RelationBelongsTo RelationKind = "belongsTo"
	RelationHasMany   RelationKind = "hasMany"
	RelationHasOne    RelationKind = "hasOne"
	RelationManyToMany RelationKind = "manyToMany"
)

// Relation describes a single relation discovered from struct tags.
type Relation struct {
	Kind       RelationKind
	FieldName  string
	TargetType reflect.Type
	ForeignKey string
	TagRaw     string
}

// ParseRelations inspects a model type and returns relations declared via
// Nimbus tags on fields. It does not configure the ORM directly, but provides
// a framework-agnostic way to inspect relationships for higher-level APIs.
//
// Tag syntax (examples):
//
//	User     User   `nimbus:"belongsTo:User,foreignKey:UserID"`
//	Posts    []Post `nimbus:"hasMany:Post,foreignKey:UserID"`
//
// You can also use a shorter key:
//
//	User User `relation:"belongsTo"`
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

	var rels []Relation

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

		// First part: kind[:Target]
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
			// Unknown kind; skip.
			continue
		}

		// Optional explicit target type name (for tooling; not used at runtime yet).
		if len(headParts) == 2 {
			_ = strings.TrimSpace(headParts[1])
		}

		// Remaining parts: key=value pairs (e.g. foreignKey:UserID).
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
			}
		}

		// Derive target type from field type (for codegen / tools).
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

		rels = append(rels, r)
	}

	return rels
}

