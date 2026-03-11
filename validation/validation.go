package validation

import (
	"encoding/json"
	"io"
)

// ValidateStruct validates a struct that implements SchemaProvider using its
// Rules() schema. Returns nil if valid, or a ValidationErrors (which also
// implements error) when validation fails.
//
// Usage:
//
//	v := &validators.Todo{Title: strings.TrimSpace(input)}
//	if err := validation.ValidateStruct(v); err != nil {
//	    // err is a ValidationErrors — render form with errors
//	}
func ValidateStruct(s any) error {
	ve := validateStruct(s)
	if ve != nil {
		return ve
	}
	return nil
}

// ValidateRequestJSON decodes JSON from body into v. No validation.
// Prefer BindAndValidateSchema or BindAndValidate instead.
func ValidateRequestJSON(body io.Reader, v any) error {
	return json.NewDecoder(body).Decode(v)
}

// decodeJSON is an internal helper for JSON decoding.
func decodeJSON(body io.Reader, v any) error {
	return json.NewDecoder(body).Decode(v)
}

// ValidationErrors holds field-level validation errors for API responses.
type ValidationErrors map[string][]string

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// ToMap returns the errors as map[string][]string for JSON responses.
func (e ValidationErrors) ToMap() map[string][]string {
	return map[string][]string(e)
}

// FormatValidationError is kept for backward compatibility.
func FormatValidationError(err error) ValidationErrors {
	if ve, ok := err.(ValidationErrors); ok {
		return ve
	}
	return nil
}
