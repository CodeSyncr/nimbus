# API Resources

A thin transformation layer between models and JSON, so the wire format is
decided in one place instead of by struct tags scattered across the codebase.

## The interface

```go
type Resource interface {
    ToJSON() map[string]any
}
```

Anything with `ToJSON()` is a resource.

## A struct resource

```go
type UserResource struct{ User *models.User }

func (r UserResource) ToJSON() map[string]any {
    return map[string]any{
        "id":         r.User.ID,
        "name":       r.User.Name,
        "created_at": r.User.CreatedAt.Format(time.RFC3339),
        // password and internal columns simply never appear
    }
}

func Show(c *http.Context) error {
    return c.JSON(200, UserResource{User: user}.ToJSON())
}
```

## A function resource

`ResourceFunc` adapts a closure to the interface:

```go
res := resource.ResourceFunc(func() map[string]any {
    return map[string]any{"id": u.ID, "name": u.Name}
})
```

## Collections

```go
items := make([]resource.Resource, 0, len(users))
for _, u := range users {
    items = append(items, UserResource{User: u})
}
return c.JSON(200, map[string]any{"data": resource.Collection(items)})
```

`resource.Collection(items []Resource) []map[string]any` maps each element
through `ToJSON`.

## Why bother

1. **Leaks are opt-in, not opt-out.** With struct tags, a new column is exposed
   the moment it is added. With a resource, a new field appears only when you
   write it.
2. **Versioning.** `UserResourceV1` and `UserResourceV2` can wrap the same model.
3. **Computed fields.** Derived values live with the presentation, not the model.

`nimbus make:resource User` scaffolds `app/resources/user.go`.

Related: [orm](orm.md) for `Serialize`, which is the struct-tag route when a
resource would be overkill.
