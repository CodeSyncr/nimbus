# Validation - Nimbus

Nimbus features a chainable validation system inspired by VineJS, offering expressive schemas and deep integration with the framework.

## Key Features

-   **Chainable Rules**: Define validation logic with fluent methods like `.Required()`, `.Min()`, `.Max()`, and `.Email()`.
-   **Struct-Based Validation**: Validate data directly against your Go structs.
-   **Form Request Pattern**: Combine payload definition, validation rules, and authorization logic.
-   **Database Rules**: Built-in rules for checking uniqueness (`Unique`) or existence (`Exists`) in the database.

## Usage

### Basic Validation

```go
type CreateUser struct {
    Email string
    Age   int
}

func (v *CreateUser) Rules() validation.Schema {
    return validation.Schema{
        "email": validation.String().Required().Email().Unique(validation.UniqueOpts{Table: "users"}),
        "age":   validation.Number().Min(18),
    }
}

err := validation.ValidateStruct(&v)
```

### Form Requests

Form requests are the recommended way to handle request validation in controllers. They implement the `validation.FormRequest` interface, allowing for automatic binding and validation.

```go
type RegisterRequest struct {
    validation.BaseFormRequest[RegisterPayload]
}

func (r *RegisterRequest) Rules() validation.Schema {
    return validation.Schema{
        "email":    validation.String().Required().Email(),
        "password": validation.String().Required().Min(8).Confirmed(),
    }
}
```

## Validation Rules

### Strings
-   `Required()`, `Min(n)`, `Max(n)`, `Email()`, `URL()`, `Alpha()`, `AlphaNum()`, `Trim()`, `Unique(opts)`, `Exists(opts)`.

### Numbers
-   `Required()`, `Min(n)`, `Max(n)`, `Positive()`, `Between(min, max)`.

### Conditional Validation
Rules can be applied conditionally using `.When(field, value, rule)` or `.WhenFn(predicate, rule)`.

## Error Handling

Validation failures return a `ValidationErrors` object, which can be easily converted to a map (`ToMap()`) for JSON responses or template rendering.

```json
{
  "email": ["Must be a valid email address", "Email already exists"],
  "password": ["Password must be at least 8 characters"]
}
```

## Best Practices

1.  **Validate Early**: Perform validation at the edge of your application (controllers).
2.  **Use Form Requests**: They provide a clean abstraction for complex validation and authorization.
3.  **Custom Messages**: Override default error messages to provide a better user experience.
