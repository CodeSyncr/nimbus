package validation

import (
	"encoding/json"
	"io"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ValidateStruct validates a struct with go-playground/validator tags (AdonisJS request.validate).
func ValidateStruct(s any) error {
	return validate.Struct(s)
}

// ValidateRequestJSON reads JSON body into v and validates (common in API handlers).
func ValidateRequestJSON(body io.Reader, v any) error {
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return err
	}
	return ValidateStruct(v)
}

// Validator errors can be unwrapped for field-level messages.
func Validator() *validator.Validate {
	return validate
}
