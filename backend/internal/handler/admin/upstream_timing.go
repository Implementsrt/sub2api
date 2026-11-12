package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamPricingHandler) Timing(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	usageID, err := strconv.ParseInt(c.Param("usageId"), 10, 64)
	if err != nil || usageID <= 0 {
		response.BadRequest(c, "用量 ID 无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	comparison, err := h.pricing.CompareTiming(ctx, id, usageID)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, comparison)
}
