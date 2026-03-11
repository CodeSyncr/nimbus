package validation

import (
	reqctx "github.com/CodeSyncr/nimbus/http"
)

// FormRequest defines a JSON form request with validation + authorization.
// T is the payload type that carries validator tags (legacy) or is
// validated via a typed Schema using BindAndValidateSchema.
//
// Example:
//
//	type LoginPayload struct {
//	  Email    string `json:"email" validate:"required,email"`
//	  Password string `json:"password" validate:"required"`
//	}
//
//	type LoginRequest struct {
//	  validation.BaseFormRequest[LoginPayload]
//	}
//
//	func (r *LoginRequest) Payload() *LoginPayload { return &LoginPayload{} }
//
//	func (r *LoginRequest) Authorize(c *http.Context) error {
//	  return nil // or return an error to deny
//	}
//
//	// In handler:
//	// req := &LoginRequest{}
//	// payload, ve, err := validation.BindAndValidate(c, req)
//	// if ve != nil { return c.JSON(422, ve.ToMap()) }
//	// if err != nil { return err } // auth failure or other error
//	// use payload.Email, payload.Password
type FormRequest[T any] interface {
	// Payload returns a pointer to the payload struct with validator tags.
	Payload() *T
	// Authorize returns an error if the request is not authorized.
	Authorize(c *reqctx.Context) error
}

// BaseFormRequest provides a no-op Authorize implementation.
// Embed it in your request type to only implement Payload().
type BaseFormRequest[T any] struct{}

func (BaseFormRequest[T]) Authorize(c *reqctx.Context) error { return nil }

// BindAndValidate binds JSON body into the payload, validates it, and runs Authorize.
// It returns:
//   - payload (*T) on success
//   - ve (ValidationErrors) when validation fails (for 422 responses)
//   - err for non-validation errors (e.g. JSON decode, authorization).
func BindAndValidate[T any](c *reqctx.Context, fr FormRequest[T]) (*T, ValidationErrors, error) {
	payload := fr.Payload()
	// Legacy behavior used struct tags; now we defer to the typed schema helper
	// when the payload implements SchemaProvider.
	if sp, ok := any(payload).(SchemaProvider); ok {
		if ve, err := BindAndValidateSchema(c, sp); ve != nil || err != nil {
			return nil, ve, err
		}
	} else {
		// Fallback: only bind JSON, no validation.
		if err := ValidateRequestJSON(c.Request.Body, payload); err != nil {
			return nil, nil, err
		}
	}
	if err := fr.Authorize(c); err != nil {
		return payload, nil, err
	}
	return payload, nil, nil
}
