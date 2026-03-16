package validation

import (
	"reflect"
)

// WhenRule applies a sub-rule only when a condition is met, providing
// conditional validation (similar to VineJS's when/otherwise pattern).
//
// Usage:
//
//	schema := validation.Schema{
//	    "role":       validation.String().Required(),
//	    "company_id": validation.When("role", "business", validation.String().Required()),
//	    "bio":        validation.WhenFn(func(data map[string]any) bool {
//	        return data["role"] == "creator"
//	    }, validation.String().Min(10)),
//	}
type WhenRule struct {
	field    string
	value    any
	condFn   func(map[string]any) bool
	then     Rule
	elseRule Rule
}

// When returns a conditional rule: apply the given rule only when the
// specified field equals the expected value.
//
//	validation.When("type", "premium", validation.String().Required().Min(10))
func When(field string, value any, then Rule) *WhenRule {
	return &WhenRule{field: field, value: value, then: then}
}

// WhenFn returns a conditional rule: apply the given rule only when predicate returns true.
//
//	validation.WhenFn(func(data map[string]any) bool {
//	    return data["plan"] == "enterprise"
//	}, validation.String().Required())
func WhenFn(fn func(map[string]any) bool, then Rule) *WhenRule {
	return &WhenRule{condFn: fn, then: then}
}

// Otherwise sets an alternative rule when the condition is NOT met.
//
//	validation.When("role", "admin", validation.String().Required()).
//	    Otherwise(validation.String().Max(100))
func (w *WhenRule) Otherwise(r Rule) *WhenRule {
	w.elseRule = r
	return w
}

// validate implements the Rule interface.
func (w *WhenRule) validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors) {
	matched := false

	if w.condFn != nil {
		// Function-based condition: extract all fields into a map.
		data := structToMap(allFields)
		matched = w.condFn(data)
	} else if w.field != "" {
		// Field-value condition: check if the specified field equals the expected value.
		depVal := getFieldValue(allFields, w.field)
		matched = reflect.DeepEqual(depVal, w.value)
	}

	if matched && w.then != nil {
		w.then.validate(field, v, allFields, msgs, out)
	} else if !matched && w.elseRule != nil {
		w.elseRule.validate(field, v, allFields, msgs, out)
	}
}

// structToMap converts a struct reflect.Value to map[string]any.
func structToMap(v reflect.Value) map[string]any {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	m := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Tag.Get("json")
		if name == "" || name == "-" {
			name = f.Name
		}
		// Strip tag options like ",omitempty".
		if idx := len(name) - 1; idx > 0 {
			for j := 0; j < len(name); j++ {
				if name[j] == ',' {
					name = name[:j]
					break
				}
			}
		}
		m[name] = v.Field(i).Interface()
	}
	return m
}

// getFieldValue extracts a field's interface value from a struct by name or json tag.
func getFieldValue(v reflect.Value, field string) any {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("json")
		if name == "" || name == "-" {
			name = f.Name
		}
		if idx := 0; idx == 0 {
			for j := 0; j < len(name); j++ {
				if name[j] == ',' {
					name = name[:j]
					break
				}
			}
		}
		if name == field || f.Name == field {
			return v.Field(i).Interface()
		}
	}
	return nil
}
