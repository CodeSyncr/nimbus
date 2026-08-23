# Routing & Controllers - Nimbus

Nimbus uses an expressive, Laravel-style routing system built on top of the Chi router, allowing for clean URL definitions and structured request handling.

## Defining Routes

Routes are typically defined in `start/routes.go`.

```go
func RegisterRoutes(app *nimbus.App) {
    app.Router.Get("/", func(c *http.Context) error {
        return c.String(200, "Hello World")
    })
}
```

## Route Groups

Group related routes to share prefixes and middleware.

```go
api := app.Router.Group("/api/v1", authMiddleware)
api.Get("/users", listUsers)
```

## Resource Controllers

Resource controllers provide a standardized way to handle CRUD operations for a resource. A single line can register up to 7 RESTful routes.

```go
app.Router.Resource("posts", &controllers.PostController{})
```

The `PostController` should implement the `ResourceController` interface (`Index`, `Create`, `Store`, `Show`, `Edit`, `Update`, `Destroy`).

## Route Parameters

Capture dynamic values from the URL.

```go
app.Router.Get("/users/:id", func(c *http.Context) error {
    id := c.Param("id")
    return c.JSON(200, map[string]string{"id": id})
})
```

## Named Routes

Assign names to routes for easy URL generation.

```go
app.Router.Get("/profile", showProfile).As("profile")
// URL generation: app.Router.URL("profile")
```

## Best Practices

1.  **Use Controllers**: Keep your `routes.go` file clean by moving logic into controller structs.
2.  **Resourceful Design**: Prefer `Resource()` for standard CRUD to maintain consistency across the application.
3.  **Strict Typing**: Use `c.ParamInt()` or `c.QueryInt()` to safely extract and convert URL parameters.
4.  **Middleware for Auth**: Apply authentication and authorization at the route or group level using middleware.
