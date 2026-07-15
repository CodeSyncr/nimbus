package admin

import (
	"fmt"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Field types understood by the form renderer.
const (
	TypeText     = "text"
	TypeTextarea = "textarea"
	TypeNumber   = "number"
	TypeBoolean  = "boolean"
	TypeEmail    = "email"
	TypePassword = "password"
	TypeDate     = "date"
	TypeSelect   = "select"
)

// Option is a choice for a select field.
type Option struct {
	Value string
	Label string
}

// Field describes one column/attribute of a resource.
type Field struct {
	Name     string // Go struct field name (also the display key)
	Label    string
	Type     string
	Options  []Option // for TypeSelect
	OnIndex  bool     // show in the list table
	OnForm   bool     // show in the create/edit form
	Readonly bool     // rendered disabled on the form
	Sortable bool
}

// ── Field constructors (chainable-ish, Nova/Filament style) ────────

func field(name, typ string) Field {
	return Field{Name: name, Label: humanize(name), Type: typ, OnIndex: true, OnForm: true}
}

// Text is a single-line text field.
func Text(name string) Field { return field(name, TypeText) }

// Textarea is a multi-line text field (hidden from the index by default).
func Textarea(name string) Field { f := field(name, TypeTextarea); f.OnIndex = false; return f }

// Number is a numeric field.
func Number(name string) Field { return field(name, TypeNumber) }

// Boolean is a checkbox field.
func Boolean(name string) Field { return field(name, TypeBoolean) }

// Email is an email input.
func Email(name string) Field { return field(name, TypeEmail) }

// Password is a write-only password field (never shown on the index, blank on edit).
func Password(name string) Field {
	f := field(name, TypePassword)
	f.OnIndex = false
	return f
}

// Date is a date input.
func Date(name string) Field { return field(name, TypeDate) }

// Select is a dropdown with fixed options.
func Select(name string, opts ...Option) Field {
	f := field(name, TypeSelect)
	f.Options = opts
	return f
}

// ── Field builder methods ──────────────────────────────────────────

// WithLabel overrides the display label.
func (f Field) WithLabel(l string) Field { f.Label = l; return f }

// HideFromIndex removes the field from the list table.
func (f Field) HideFromIndex() Field { f.OnIndex = false; return f }

// HideFromForm removes the field from create/edit forms.
func (f Field) HideFromForm() Field { f.OnForm = false; return f }

// AsReadonly renders the field disabled on the form.
func (f Field) AsReadonly() Field { f.Readonly = true; return f }

// AsSortable marks the field sortable in the index.
func (f Field) AsSortable() Field { f.Sortable = true; return f }

// ── Resource ───────────────────────────────────────────────────────

// Resource maps a database model to an admin CRUD screen.
type Resource struct {
	// Model is a pointer to a zero value of the model, e.g. &models.Post{}.
	Model any
	// Label is the plural display name ("Posts"). Defaults from the type name.
	Label string
	// Singular display name ("Post").
	Singular string
	// Slug is the URL segment ("posts"). Defaults from the type name.
	Slug string
	// Fields to display. When empty, fields are inferred from the struct.
	Fields []Field
	// PerPage rows on the index. Default 15.
	PerPage int

	typ reflect.Type // cached struct type
}

// normalize fills defaults (slug, labels, inferred fields, cached type).
func (r *Resource) normalize() {
	r.typ = derefType(reflect.TypeOf(r.Model))
	name := r.typ.Name()
	if r.Singular == "" {
		r.Singular = humanize(name)
	}
	if r.Label == "" {
		r.Label = r.Singular + "s"
	}
	if r.Slug == "" {
		r.Slug = strings.ToLower(name) + "s"
	}
	if r.PerPage == 0 {
		r.PerPage = 15
	}
	if len(r.Fields) == 0 {
		r.Fields = inferFields(r.typ)
	}
}

// indexFields / formFields filter by visibility.
func (r *Resource) indexFields() []Field { return filterFields(r.Fields, func(f Field) bool { return f.OnIndex }) }
func (r *Resource) formFields() []Field  { return filterFields(r.Fields, func(f Field) bool { return f.OnForm }) }

// newPtr returns a pointer to a fresh zero model (as any).
func (r *Resource) newPtr() any { return reflect.New(r.typ).Interface() }

// newSlicePtr returns a pointer to a fresh []Model (as any) for db.Find.
func (r *Resource) newSlicePtr() any {
	return reflect.New(reflect.SliceOf(r.typ)).Interface()
}

// ── Reflection helpers ─────────────────────────────────────────────

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// fieldStringValue reads a struct field by name and renders it for display.
func fieldStringValue(model reflect.Value, name string) string {
	model = reflect.Indirect(model)
	fv := model.FieldByName(name)
	if !fv.IsValid() {
		return ""
	}
	return displayValue(fv.Interface())
}

func displayValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.Format("2006-01-02 15:04")
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// setField assigns a form string to a struct field, converting to its kind.
func setField(model reflect.Value, name, raw string) error {
	model = reflect.Indirect(model)
	fv := model.FieldByName(name)
	if !fv.IsValid() || !fv.CanSet() {
		return nil // unknown/unsettable fields are ignored
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		fv.SetBool(raw == "1" || raw == "true" || raw == "on")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if raw == "" {
			fv.SetInt(0)
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if raw == "" {
			fv.SetUint(0)
			return nil
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if raw == "" {
			fv.SetFloat(0)
			return nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fv.SetFloat(f)
	}
	return nil
}

// inferFields builds a default field set from exported struct fields, skipping
// the embedded base-model bookkeeping columns.
func inferFields(t reflect.Type) []Field {
	skip := map[string]bool{"ID": true, "CreatedAt": true, "UpdatedAt": true, "DeletedAt": true, "Model": true}
	var out []Field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous || !sf.IsExported() || skip[sf.Name] {
			continue
		}
		out = append(out, guessField(sf))
	}
	return out
}

// guessField picks a field type from the Go kind and name.
func guessField(sf reflect.StructField) Field {
	name := strings.ToLower(sf.Name)
	switch {
	case sf.Type.Kind() == reflect.Bool:
		return Boolean(sf.Name)
	case strings.Contains(name, "email"):
		return Email(sf.Name)
	case strings.Contains(name, "password"):
		return Password(sf.Name)
	case strings.Contains(name, "body") || strings.Contains(name, "description") || strings.Contains(name, "content"):
		return Textarea(sf.Name)
	case isIntKind(sf.Type.Kind()) || isFloatKind(sf.Type.Kind()):
		return Number(sf.Name)
	case sf.Type == reflect.TypeOf(time.Time{}):
		return Date(sf.Name)
	default:
		return Text(sf.Name)
	}
}

func isIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

func filterFields(in []Field, keep func(Field) bool) []Field {
	var out []Field
	for _, f := range in {
		if keep(f) {
			out = append(out, f)
		}
	}
	return out
}

// humanize turns "PublishedAt" / "published_at" into "Published At".
func humanize(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return out
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

// renderInput builds the HTML control for a form field with the current value.
func renderInput(f Field, value string) template.HTML {
	disabled := ""
	if f.Readonly {
		disabled = " disabled"
	}
	esc := template.HTMLEscapeString
	switch f.Type {
	case TypeTextarea:
		return template.HTML(fmt.Sprintf(
			`<textarea name="%s" rows="5" class="%s"%s>%s</textarea>`,
			esc(f.Name), inputClass, disabled, esc(value)))
	case TypeBoolean:
		checked := ""
		if value == "Yes" || value == "1" || value == "true" {
			checked = " checked"
		}
		return template.HTML(fmt.Sprintf(
			`<input type="hidden" name="%s" value="0"/><label class="inline-flex items-center gap-2"><input type="checkbox" name="%s" value="1"%s%s class="w-4 h-4"/><span class="text-sm text-slate-500">Enabled</span></label>`,
			esc(f.Name), esc(f.Name), checked, disabled))
	case TypeSelect:
		var b strings.Builder
		fmt.Fprintf(&b, `<select name="%s" class="%s"%s>`, esc(f.Name), inputClass, disabled)
		for _, o := range f.Options {
			sel := ""
			if o.Value == value {
				sel = " selected"
			}
			fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, esc(o.Value), sel, esc(o.Label))
		}
		b.WriteString(`</select>`)
		return template.HTML(b.String())
	default:
		it := "text"
		switch f.Type {
		case TypeNumber:
			it = "number"
		case TypeEmail:
			it = "email"
		case TypePassword:
			it = "password"
		case TypeDate:
			it = "date"
		}
		val := value
		if f.Type == TypePassword {
			val = "" // never echo secrets back
		}
		return template.HTML(fmt.Sprintf(
			`<input type="%s" name="%s" value="%s" class="%s"%s/>`,
			it, esc(f.Name), esc(val), inputClass, disabled))
	}
}

const inputClass = "w-full px-3 py-2 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-800 text-slate-900 dark:text-white focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 outline-none"
