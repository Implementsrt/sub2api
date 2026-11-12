package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// Called inside the existing admin authentication, audit and rate-limit group.
func registerUpstreamPricingRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin.UpstreamPricing == nil {
		return
	}
	p := admin.Group("/upstream-pricing/accounts/:id")
	v := h.Admin.UpstreamPricing
	p.GET("", v.Get)
	p.GET("/usage/:usageId/timing", v.Timing)
	p.POST("/groups", v.Groups)
	p.POST("/preview", v.Preview)
	p.POST("/apply", v.Apply)
	p.POST("/sync", v.Sync)
	p.PUT("/schedule", v.Schedule)
	p.POST("/rollback-preview", v.RollbackPreview)
}
