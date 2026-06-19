package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler returns a handler for health checks.
//
// @Summary      Health check
// @Description  Returns service health status and running version.
// @Tags         System
// @Produce      json
// @Success      200  {object}  HealthResponseDoc
// @Router       /health [get]
func HealthHandler(version string) gin.HandlerFunc {
	type resp struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	return func(c *gin.Context) {
		writeJSON(c, http.StatusOK, resp{Status: "ok", Version: version})
	}
}
