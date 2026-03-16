/*
|--------------------------------------------------------------------------
| Workflow Engine — Nimbus Plugin
|--------------------------------------------------------------------------
|
| Integrates the workflow engine with the Nimbus plugin system.
| Provides routes for inspecting and managing workflows.
|
*/

package workflow

import (
	"encoding/json"
	"net/http"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin    = (*WorkflowPlugin)(nil)
	_ nimbus.HasRoutes = (*WorkflowPlugin)(nil)
	_ nimbus.HasConfig = (*WorkflowPlugin)(nil)
)

// WorkflowPlugin integrates the workflow engine with Nimbus.
type WorkflowPlugin struct {
	nimbus.BasePlugin
	Engine *Engine
}

// NewPlugin creates a new workflow plugin.
func NewPlugin(store Store) *WorkflowPlugin {
	return &WorkflowPlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "workflow",
			PluginVersion: "1.0.0",
		},
		Engine: NewEngine(store),
	}
}

func (p *WorkflowPlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("workflow.engine", func() *Engine { return p.Engine })
	return nil
}

func (p *WorkflowPlugin) Boot(app *nimbus.App) error {
	return nil
}

func (p *WorkflowPlugin) DefaultConfig() map[string]any {
	return map[string]any{
		"store": "memory",
	}
}

// RegisterRoutes mounts workflow API routes.
func (p *WorkflowPlugin) RegisterRoutes(r *router.Router) {
	grp := r.Group("/_workflow")
	grp.Get("/", p.listWorkflows)
	grp.Get("/:name/runs", p.listRuns)
	grp.Get("/runs/:id", p.getStatus)
	grp.Post("/:name/dispatch", p.dispatch)
	grp.Post("/runs/:id/signal", p.signal)
	grp.Post("/runs/:id/cancel", p.cancel)
}

func (p *WorkflowPlugin) listWorkflows(c *nhttp.Context) error {
	workflows := p.Engine.Workflows()
	return c.JSON(http.StatusOK, map[string]any{
		"workflows": workflows,
	})
}

func (p *WorkflowPlugin) listRuns(c *nhttp.Context) error {
	name := c.Param("name")
	runs, err := p.Engine.List(c.Request.Context(), name, 50)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, runs)
}

func (p *WorkflowPlugin) getStatus(c *nhttp.Context) error {
	id := c.Param("id")
	run, err := p.Engine.Status(c.Request.Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, run)
}

func (p *WorkflowPlugin) dispatch(c *nhttp.Context) error {
	name := c.Param("name")
	var payload Payload
	if err := json.NewDecoder(c.Request.Body).Decode(&payload); err != nil {
		payload = Payload{}
	}
	id, err := p.Engine.Dispatch(name, payload)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusAccepted, map[string]string{
		"run_id":   id,
		"workflow": name,
		"status":   string(RunPending),
	})
}

func (p *WorkflowPlugin) signal(c *nhttp.Context) error {
	id := c.Param("id")
	var body struct {
		Event string  `json:"event"`
		Data  Payload `json:"data"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if err := p.Engine.Signal(id, body.Event, body.Data); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "signalled"})
}

func (p *WorkflowPlugin) cancel(c *nhttp.Context) error {
	id := c.Param("id")
	if err := p.Engine.Cancel(c.Request.Context(), id); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "cancelled"})
}
