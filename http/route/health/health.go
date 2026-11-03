// Package health exposes the liveness probe.
package health

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

const PathHealthz = "healthz"

type Status struct {
	Status string `json:"status"`
}

// Register adds the probes to the group mounted on the base path.
func Register(g *echo.Group) {
	g.GET("/"+PathHealthz, Healthz)
}

// Healthz answers as long as the process serves HTTP.
func Healthz(c *echo.Context) error {
	return c.JSON(http.StatusOK, Status{Status: "ok"})
}
