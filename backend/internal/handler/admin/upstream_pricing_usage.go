package admin

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *UsageHandler) upstreamPricingRows(c *gin.Context, records []service.UsageLog) any {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		return adminUsageLogResponses(c, records)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	estimates, _ := h.usageService.UpstreamEstimates(ctx, 0, records)
	type row struct {
		dto.AdminUsageLog
		UpstreamEstimate *upstreampricing.Estimate `json:"upstream_estimate,omitempty"`
	}
	out := make([]row, 0, len(records))
	for i := range records {
		out = append(out, row{*dto.UsageLogFromServiceAdmin(&records[i]), estimates[records[i].ID]})
	}
	return out
}
