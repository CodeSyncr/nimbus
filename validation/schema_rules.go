package validation

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	reqctx "github.com/CodeSyncr/nimbus/http"
)

// Schema defines typed validation rules for a request payload.
// Keys are field names (preferring json tag names).
type Schema map[string]Rule

// Rule is implemented by all typed rules.
type Rule interface {
	validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors)
}

// ── String Rule ─────────────────────────────────────────────────

// StringRule validates string fields with a chainable, VineJS-style API.
type StringRule struct {
	required   bool
	min        *int
	max        *int
	email      bool
	urlRule    bool
	alpha      bool
	alphaNum   bool
	trim       bool
	regex      *regexp.Regexp
	inValues   []string
	confirmed  bool
	uniqueOpts *UniqueOpts
	existsOpts *ExistsOpts
}

// String creates a new StringRule.
func String() *StringRule {
	return &StringRule{}
}

// Required marks the field as required (non-empty).
func (r *StringRule) Required() *StringRule {
	r.required = true
	return r
}

// Min sets the minimum length.
func (r *StringRule) Min(n int) *StringRule {
	r.min = &n
	return r
}

// Max sets the maximum length.
func (r *StringRule) Max(n int) *StringRule {
	r.max = &n
	return r
}

// Email validates the field as an email address.
func (r *StringRule) Email() *StringRule {
	r.email = true
	return r
}

// URL validates the field as an absolute URL.
func (r *StringRule) URL() *StringRule {
	r.urlRule = true
	return r
}

// Alpha validates the field contains only letters.
func (r *StringRule) Alpha() *StringRule {
	r.alpha = true
	return r
}

// AlphaNum validates the field contains only letters and digits.
func (r *StringRule) AlphaNum() *StringRule {
	r.alphaNum = true
	return r
}

// Trim trims whitespace before validation.
func (r *StringRule) Trim() *StringRule {
	r.trim = true
	return r
}

// Regex validates the field matches the given pattern.
func (r *StringRule) Regex(pattern string) *StringRule {
	r.regex = regexp.MustCompile(pattern)
	return r
}

// In validates the field value is one of the allowed values.
func (r *StringRule) In(values ...string) *StringRule {
	r.inValues = values
	return r
}

// Confirmed validates that a matching {field}_confirmation field exists and
// has the same value. The confirmation field is looked up in the parent struct.
func (r *StringRule) Confirmed() *StringRule {
	r.confirmed = true
	return r
}

// Unique validates uniqueness in the database. See UniqueOpts.
func (r *StringRule) Unique(opts UniqueOpts) *StringRule {
	r.uniqueOpts = &opts
	return r
}

// Exists validates that the value exists in the database. See ExistsOpts.
func (r *StringRule) Exists(opts ExistsOpts) *StringRule {
	r.existsOpts = &opts
	return r
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
var alphaRegex = regexp.MustCompile(`^[a-zA-Z]+$`)
var alphaNumRegex = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

func (r *StringRule) validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors) {
	if v.Kind() != reflect.String {
		return
	}
	val := v.String()

	// Trim before validation.
	if r.trim {
		val = strings.TrimSpace(val)
	}

	// Required check.
	if r.required && val == "" {
		addRuleError(out, field, "required", msgs)
		return // stop on required failure
	}

	// Skip further checks if empty and not required.
	if val == "" {
		return
	}

	if r.min != nil && len(val) < *r.min {
		addRuleError(out, field, "min", msgs)
	}
	if r.max != nil && len(val) > *r.max {
		addRuleError(out, field, "max", msgs)
	}
	if r.email && !emailRegex.MatchString(val) {
		addRuleError(out, field, "email", msgs)
	}
	if r.urlRule {
		u, err := url.ParseRequestURI(val)
		if err != nil || u.Scheme == "" || u.Host == "" {
			addRuleError(out, field, "url", msgs)
		}
	}
	if r.alpha && !alphaRegex.MatchString(val) {
		addRuleError(out, field, "alpha", msgs)
	}
	if r.alphaNum && !alphaNumRegex.MatchString(val) {
		addRuleError(out, field, "alphaNum", msgs)
	}
	if r.regex != nil && !r.regex.MatchString(val) {
		addRuleError(out, field, "regex", msgs)
	}
	if len(r.inValues) > 0 {
		found := false
		for _, allowed := range r.inValues {
			if val == allowed {
				found = true
				break
			}
		}
		if !found {
			addRuleError(out, field, "in", msgs)
		}
	}
	if r.confirmed && allFields.Kind() == reflect.Struct {
		confField := findFieldValue(allFields, field+"_confirmation")
		if confField.Kind() != reflect.String || confField.String() != val {
			addRuleError(out, field, "confirmed", msgs)
		}
	}

	// Database rules.
	if r.uniqueOpts != nil {
		if err := checkUnique(*r.uniqueOpts, field, val); err != nil {
			addRuleError(out, field, "unique", msgs)
		}
	}
	if r.existsOpts != nil {
		if err := checkExists(*r.existsOpts, field, val); err != nil {
			addRuleError(out, field, "exists", msgs)
		}
	}
}

// ── Number Rule ─────────────────────────────────────────────────

// NumberRule validates numeric fields (int, uint, float).
type NumberRule struct {
	required bool
	min      *float64
	max      *float64
	positive bool
}

// Number creates a new NumberRule.
func Number() *NumberRule {
	return &NumberRule{}
}

func (r *NumberRule) Required() *NumberRule {
	r.required = true
	return r
}

func (r *NumberRule) Min(n float64) *NumberRule {
	r.min = &n
	return r
}

func (r *NumberRule) Max(n float64) *NumberRule {
	r.max = &n
	return r
}

func (r *NumberRule) Positive() *NumberRule {
	r.positive = true
	return r
}

func (r *NumberRule) Between(a, b float64) *NumberRule {
	r.min = &a
	r.max = &b
	return r
}

func (r *NumberRule) validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors) {
	var val float64
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = float64(v.Int())
		if r.required && v.Int() == 0 {
			addRuleError(out, field, "required", msgs)
			return
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val = float64(v.Uint())
		if r.required && v.Uint() == 0 {
			addRuleError(out, field, "required", msgs)
			return
		}
	case reflect.Float32, reflect.Float64:
		val = v.Float()
		if r.required && val == 0 {
			addRuleError(out, field, "required", msgs)
			return
		}
	default:
		addRuleError(out, field, "number", msgs)
		return
	}
	if r.min != nil && val < *r.min {
		addRuleError(out, field, "min", msgs)
	}
	if r.max != nil && val > *r.max {
		addRuleError(out, field, "max", msgs)
	}
	if r.positive && val <= 0 {
		addRuleError(out, field, "positive", msgs)
	}
}

// ── Bool Rule ───────────────────────────────────────────────────

// BoolRule validates boolean fields.
type BoolRule struct{}

func Bool() *BoolRule { return &BoolRule{} }

func (r *BoolRule) validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors) {
	if v.Kind() != reflect.Bool {
		addRuleError(out, field, "bool", msgs)
	}
}

// ── UInt Rule (legacy, use NumberRule instead) ───────────────────

// UIntRule validates unsigned integer fields.
type UIntRule struct {
	required bool
}

func UInt() *UIntRule { return &UIntRule{} }

func (r *UIntRule) Required() *UIntRule {
	r.required = true
	return r
}

func (r *UIntRule) validate(field string, v reflect.Value, allFields reflect.Value, msgs map[string]string, out ValidationErrors) {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if r.required && v.Uint() == 0 {
			addRuleError(out, field, "required", msgs)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if r.required && v.Int() == 0 {
			addRuleError(out, field, "required", msgs)
		}
	default:
		addRuleError(out, field, "uint", msgs)
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func addRuleError(out ValidationErrors, field, rule string, msgs map[string]string) {
	if out == nil {
		return
	}
	key := field + "." + rule
	msg, ok := msgs[key]
	if !ok {
		switch rule {
		case "required":
			msg = field + " is required"
		case "min":
			msg = field + " is too short"
		case "max":
			msg = field + " is too long"
		case "email":
			msg = field + " must be a valid email address"
		case "url":
			msg = field + " must be a valid URL"
		case "alpha":
			msg = field + " must contain only letters"
		case "alphaNum":
			msg = field + " must contain only letters and numbers"
		case "regex":
			msg = field + " format is invalid"
		case "in":
			msg = field + " is not an allowed value"
		case "confirmed":
			msg = field + " confirmation does not match"
		case "unique":
			msg = field + " has already been taken"
		case "exists":
			msg = field + " does not exist"
		case "number":
			msg = field + " must be a number"
		case "positive":
			msg = field + " must be positive"
		default:
			msg = field + " " + rule
		}
	}
	out[field] = append(out[field], msg)
}

// findFieldValue looks up a struct field by json tag or Go name.
func findFieldValue(rv reflect.Value, name string) reflect.Value {
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		jsonTag := sf.Tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]
		formTag := sf.Tag.Get("form")
		if jsonName == name || formTag == name || sf.Name == name {
			return rv.Field(i)
		}
	}
	return reflect.Value{}
}

// ── Interfaces ──────────────────────────────────────────────────

// SchemaProvider is implemented by request types that provide typed rules.
type SchemaProvider interface {
	Rules() Schema
}

// MessageProvider is implemented by request types that provide custom messages.
type MessageProvider interface {
	Messages() map[string]string
}

// Authorizer is implemented by request types that perform authorization.
type Authorizer interface {
	Authorize(c *reqctx.Context) error
}

// Preparer is implemented by request types that need pre-validation sanitization.
type Preparer interface {
	Prepare()
}

// ── BindAndValidateSchema ───────────────────────────────────────

// BindAndValidateSchema binds JSON body into req, runs Prepare (if present),
// validates using typed Schema rules, and then calls Authorize (if present).
func BindAndValidateSchema(c *reqctx.Context, req any) (ValidationErrors, error) {
	if req == nil {
		return nil, fmt.Errorf("validation: BindAndValidateSchema req is nil")
	}

	// Bind JSON into req.
	if err := decodeBody(c, req); err != nil {
		return nil, err
	}

	// Prepare (sanitize) if supported.
	if p, ok := req.(Preparer); ok {
		p.Prepare()
	}

	ve := validateStruct(req)
	if len(ve) > 0 {
		return ve, nil
	}

	// Authorization if supported.
	if a, ok := req.(Authorizer); ok {
		if err := a.Authorize(c); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// validateStruct runs schema rules against a struct's fields.
// This is the core engine used by both BindAndValidateSchema and Validate().
func validateStruct(req any) ValidationErrors {
	sp, ok := req.(SchemaProvider)
	if !ok {
		return nil
	}
	schema := sp.Rules()

	msgs := map[string]string{}
	if mp, ok := req.(MessageProvider); ok {
		msgs = mp.Messages()
	}

	ve := make(ValidationErrors)

	rv := reflect.ValueOf(req)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()

	for fieldName, rule := range schema {
		var fv reflect.Value
		found := false
		for i := 0; i < rt.NumField(); i++ {
			sf := rt.Field(i)
			jsonTag := sf.Tag.Get("json")
			jsonName := strings.Split(jsonTag, ",")[0]
			formTag := sf.Tag.Get("form")
			if jsonName == fieldName || formTag == fieldName || (jsonName == "" && formTag == "" && sf.Name == fieldName) {
				fv = rv.Field(i)
				found = true
				break
			}
		}
		if !found {
			continue
		}
		rule.validate(fieldName, fv, rv, msgs, ve)
	}

	if len(ve) > 0 {
		return ve
	}
	return nil
}

// decodeBody decodes JSON from the request body into req.
func decodeBody(c *reqctx.Context, req any) error {
	if c.Request.Body == nil {
		return nil
	}
	ct := c.Request.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") || ct == "" {
		return decodeJSON(c.Request.Body, req)
	}
	// For form data, we don't auto-bind — the user populates the struct manually.
	return nil
}
