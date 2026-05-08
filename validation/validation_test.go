package validation

import (
	"reflect"
	"testing"
	"time"
)

// ── String Rules ────────────────────────────────────────────────

func TestStringRequired_Empty(t *testing.T) {
	type Req struct {
		Name string `json:"name"`
	}
	err := validatePayload(Req{Name: ""}, Schema{"name": String().Required()})
	if err == nil {
		t.Fatal("expected validation error for empty required string")
	}
	if _, ok := err["name"]; !ok {
		t.Error("expected error on 'name' field")
	}
}

func TestStringRequired_Present(t *testing.T) {
	type Req struct {
		Name string
	}
	err := validatePayload(Req{Name: "Alice"}, Schema{"Name": String().Required()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStringMin(t *testing.T) {
	tests := []struct {
		name  string
		value string
		min   int
		fail  bool
	}{
		{"too short", "ab", 3, true},
		{"exact", "abc", 3, false},
		{"longer", "abcd", 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type Req struct {
				S string
			}
			err := validatePayload(Req{S: tt.value}, Schema{"S": String().Min(tt.min)})
			if tt.fail && err == nil {
				t.Error("expected validation error")
			}
			if !tt.fail && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestStringMax(t *testing.T) {
	type Req struct {
		S string
	}
	err := validatePayload(Req{S: "toolong"}, Schema{"S": String().Max(3)})
	if err == nil {
		t.Fatal("expected validation error for string exceeding max")
	}
}

func TestStringEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"user@example.com", true},
		{"user@sub.domain.com", true},
		{"invalid", false},
		{"@missing.com", false},
		{"no-at-sign", false},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			type Req struct {
				E string
			}
			err := validatePayload(Req{E: tt.email}, Schema{"E": String().Email()})
			if tt.valid && err != nil {
				t.Errorf("expected valid email, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid email to fail")
			}
		})
	}
}

func TestStringURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"https://example.com", true},
		{"http://localhost:3000", true},
		{"not-a-url", false},
		{"ftp://files.example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			type Req struct {
				U string
			}
			err := validatePayload(Req{U: tt.url}, Schema{"U": String().URL()})
			if tt.valid && err != nil {
				t.Errorf("expected valid URL, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid URL to fail")
			}
		})
	}
}

func TestStringAlpha(t *testing.T) {
	type Req struct {
		S string
	}
	err := validatePayload(Req{S: "abc123"}, Schema{"S": String().Alpha()})
	if err == nil {
		t.Fatal("expected alpha validation to fail for 'abc123'")
	}
}

func TestStringAlphaNum(t *testing.T) {
	type Req struct {
		S string
	}
	err := validatePayload(Req{S: "abc123"}, Schema{"S": String().AlphaNum()})
	if err != nil {
		t.Errorf("'abc123' should pass alphaNum: %v", err)
	}
}

func TestStringTrim(t *testing.T) {
	type Req struct {
		S string
	}
	// After trim, "  abc  " → "abc" (3 chars), should fail Min(4)
	err := validatePayload(Req{S: "  abc  "}, Schema{"S": String().Trim().Min(4)})
	if err == nil {
		t.Fatal("expected min failure after trim")
	}
}

func TestStringIn(t *testing.T) {
	type Req struct {
		Role string
	}
	err := validatePayload(Req{Role: "moderator"}, Schema{"Role": String().In("admin", "user")})
	if err == nil {
		t.Fatal("expected In validation to fail for 'moderator'")
	}
}

func TestStringConfirmed(t *testing.T) {
	type Req struct {
		Password              string `json:"password"`
		Password_confirmation string `json:"password_confirmation"`
	}
	err := validatePayload(
		Req{Password: "secret", Password_confirmation: "different"},
		Schema{"password": String().Confirmed()},
	)
	if err == nil {
		t.Fatal("expected confirmed validation to fail when passwords don't match")
	}
}

func TestStringRegex(t *testing.T) {
	type Req struct {
		Code string
	}
	err := validatePayload(Req{Code: "abc"}, Schema{"Code": String().Regex(`^\d+$`)})
	if err == nil {
		t.Fatal("expected regex validation to fail for non-digits")
	}
	err = validatePayload(Req{Code: "123"}, Schema{"Code": String().Regex(`^\d+$`)})
	if err != nil {
		t.Errorf("expected '123' to pass digit regex: %v", err)
	}
}

// ── Number Rules ────────────────────────────────────────────────

func TestNumberRequired_Zero(t *testing.T) {
	type Req struct {
		Age int
	}
	err := validatePayload(Req{Age: 0}, Schema{"Age": Number().Required()})
	if err == nil {
		t.Fatal("expected required number validation to fail for zero")
	}
}

func TestNumberMin(t *testing.T) {
	type Req struct {
		Age int
	}
	err := validatePayload(Req{Age: 15}, Schema{"Age": Number().Min(18)})
	if err == nil {
		t.Fatal("expected min validation to fail")
	}
}

func TestNumberMax(t *testing.T) {
	type Req struct {
		Score float64
	}
	err := validatePayload(Req{Score: 105.0}, Schema{"Score": Number().Max(100)})
	if err == nil {
		t.Fatal("expected max validation to fail")
	}
}

func TestNumberPositive(t *testing.T) {
	type Req struct {
		Amount float64
	}
	err := validatePayload(Req{Amount: -5.0}, Schema{"Amount": Number().Positive()})
	if err == nil {
		t.Fatal("expected positive validation to fail for negative")
	}
}

func TestNumberBetween_InRange(t *testing.T) {
	type Req struct {
		Rating int
	}
	err := validatePayload(Req{Rating: 3}, Schema{"Rating": Number().Between(1, 5)})
	if err != nil {
		t.Errorf("rating 3 should pass Between(1,5): %v", err)
	}
}

// ── Date Rules ──────────────────────────────────────────────────

func TestDateRequired_Empty(t *testing.T) {
	type Req struct {
		DOB string
	}
	err := validatePayload(Req{DOB: ""}, Schema{"DOB": Date().Required()})
	if err == nil {
		t.Fatal("expected required date to fail for empty string")
	}
}

func TestDateBefore(t *testing.T) {
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	type Req struct {
		Date string
	}
	err := validatePayload(Req{Date: "2026-06-15T00:00:00Z"}, Schema{"Date": Date().Before(cutoff)})
	if err == nil {
		t.Fatal("expected Before validation to fail for future date")
	}
}

func TestDateAfter(t *testing.T) {
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	type Req struct {
		Date string
	}
	err := validatePayload(Req{Date: "2026-06-15T00:00:00Z"}, Schema{"Date": Date().After(cutoff)})
	if err != nil {
		t.Errorf("expected date after cutoff to pass: %v", err)
	}
}

// ── Array Rules ─────────────────────────────────────────────────

func TestArrayRequired_Empty(t *testing.T) {
	type Req struct {
		Tags []string
	}
	err := validatePayload(Req{Tags: []string{}}, Schema{"Tags": Array().Required()})
	if err == nil {
		t.Fatal("expected required array to fail for empty slice")
	}
}

func TestArrayMin(t *testing.T) {
	type Req struct {
		Items []int
	}
	err := validatePayload(Req{Items: []int{1}}, Schema{"Items": Array().Min(2)})
	if err == nil {
		t.Fatal("expected array min validation to fail")
	}
}

// ── Conditional Rules ───────────────────────────────────────────

func TestWhen_ConditionMet(t *testing.T) {
	type Req struct {
		Role      string `json:"role"`
		CompanyID string `json:"company_id"`
	}
	err := validatePayload(
		Req{Role: "business", CompanyID: ""},
		Schema{"company_id": When("role", "business", String().Required())},
	)
	if err == nil {
		t.Fatal("expected conditional required to fail when condition is met")
	}
}

func TestWhen_ConditionNotMet(t *testing.T) {
	type Req struct {
		Role      string `json:"role"`
		CompanyID string `json:"company_id"`
	}
	err := validatePayload(
		Req{Role: "user", CompanyID: ""},
		Schema{"company_id": When("role", "business", String().Required())},
	)
	if err != nil {
		t.Fatalf("should pass when condition not met: %v", err)
	}
}

func TestWhenFn(t *testing.T) {
	type Req struct {
		Plan string `json:"plan"`
		SLA  string `json:"sla"`
	}
	err := validatePayload(
		Req{Plan: "enterprise", SLA: ""},
		Schema{
			"sla": WhenFn(func(data map[string]any) bool {
				return data["plan"] == "enterprise"
			}, String().Required()),
		},
	)
	if err == nil {
		t.Fatal("expected WhenFn conditional required to fail")
	}
}

// ── ValidationErrors ────────────────────────────────────────────

func TestValidationErrors_ToMap(t *testing.T) {
	ve := ValidationErrors{
		"email": {"must be valid", "already taken"},
	}
	m := ve.ToMap()
	if len(m["email"]) != 2 {
		t.Errorf("expected 2 errors for email, got %d", len(m["email"]))
	}
}

func TestValidationErrors_ErrorInterface(t *testing.T) {
	ve := ValidationErrors{"name": {"required"}}
	var err error = ve
	if err.Error() == "" {
		t.Error("expected non-empty error string")
	}
}

func TestFormatValidationError_VE(t *testing.T) {
	ve := ValidationErrors{"field": {"error"}}
	result := FormatValidationError(ve)
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestFormatValidationError_Nil(t *testing.T) {
	if FormatValidationError(nil) != nil {
		t.Error("expected nil for non-ValidationErrors")
	}
}

// ── Valid Struct (no errors) ────────────────────────────────────

func TestValidStruct_Passes(t *testing.T) {
	type Req struct {
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
	err := validatePayload(
		Req{Email: "user@example.com", Age: 25},
		Schema{
			"email": String().Required().Email(),
			"age":   Number().Required().Min(18),
		},
	)
	if err != nil {
		t.Fatalf("expected valid struct to pass: %v", err)
	}
}

// ── Multiple Errors ─────────────────────────────────────────────

func TestMultipleFieldErrors(t *testing.T) {
	type Req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	err := validatePayload(
		Req{Email: "", Name: ""},
		Schema{
			"email": String().Required(),
			"name":  String().Required(),
		},
	)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	if len(err) < 2 {
		t.Errorf("expected at least 2 field errors, got %d", len(err))
	}
}

// ── Non-Required Empty Skipped ──────────────────────────────────

func TestNonRequired_EmptySkipped(t *testing.T) {
	type Req struct {
		Bio string
	}
	err := validatePayload(Req{Bio: ""}, Schema{"Bio": String().Min(10).Email()})
	if err != nil {
		t.Fatalf("non-required empty field should skip validation: %v", err)
	}
}

// ── Test Helper ─────────────────────────────────────────────────
// validatePayload directly invokes the validation engine on a struct value
// and a schema, bypassing the SchemaProvider interface approach.

func validatePayload(req any, schema Schema) ValidationErrors {
	ve := make(ValidationErrors)
	rv := reflect.ValueOf(req)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()

	for fieldName, rule := range schema {
		var fv reflect.Value
		found := false
		for i := 0; i < rt.NumField(); i++ {
			sf := rt.Field(i)
			jsonTag := sf.Tag.Get("json")
			jsonName := jsonTag
			if idx := len(jsonName); idx > 0 {
				for j := 0; j < len(jsonName); j++ {
					if jsonName[j] == ',' {
						jsonName = jsonName[:j]
						break
					}
				}
			}
			if jsonName == fieldName || sf.Name == fieldName {
				fv = rv.Field(i)
				found = true
				break
			}
		}
		if !found {
			continue
		}
		rule.validate(fieldName, fv, rv, map[string]string{}, ve)
	}

	if len(ve) > 0 {
		return ve
	}
	return nil
}
