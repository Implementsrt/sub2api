package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamPricingHandler) Comparison(c *gin.Context) {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.Error(c, 403, "上游价格对比仅供完整管理员查看")
		return
	}
	if h.pricing == nil || h.pricing.Manager == nil {
		response.Error(c, 503, "上游定价服务未启用")
		return
	}
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	account, e3 := strconv.ParseInt(c.DefaultQuery("account_id", "0"), 10, 64)
	search := strings.TrimSpace(c.Query("model"))
	if e1 != nil || e2 != nil || e3 != nil || page < 1 || page > 100000 || size < 1 || size > 200 || account < 0 || len(search) > 300 {
		response.BadRequest(c, "筛选或分页参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	value, err := h.pricing.Store.Comparison(ctx, search, account, page, size)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, value)
}
