// Example Nimbus app: minimal routes and middleware.
package main

import (
	"net/http"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/middleware"
)

func main() {
	app := nimbus.New()
	app.Router.Use(middleware.Logger(), middleware.Recover())

	app.Router.Get("/", func(c *context.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"message": "Hello, Nimbus!"})
	})
	app.Router.Get("/users/:id", func(c *context.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"user_id": c.Param("id")})
	})

	api := app.Router.Group("/api")
	api.Get("/health", func(c *context.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	_ = app.Run()
}
