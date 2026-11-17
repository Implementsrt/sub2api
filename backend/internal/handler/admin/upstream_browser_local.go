package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamPricingHandler) LocalBrowserImport(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	owner, ok := browserOwner(c)
	if !ok {
		return
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	proxyHeaders := c.GetHeader("Forwarded") != "" || c.GetHeader("X-Forwarded-For") != "" || c.GetHeader("X-Real-IP") != ""
	if err := upstreampricing.ValidateLocalBrowserRequest(c.GetHeader("Origin"), c.Request.Host, scheme, c.Request.RemoteAddr); err != nil || proxyHeaders {
		response.Error(c, 403, "本机浏览器读取需要从本机 Sub2API 页面直接发起")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var body struct {
		Revision int64                  `json:"revision"`
		Config   upstreampricing.Config `json:"config"`
	}
	if c.ShouldBindJSON(&body) != nil {
		response.BadRequest(c, "请求格式无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	value, err := h.pricing.ImportLocalFirefox(ctx, owner, id, body.Revision, c.GetHeader("Origin"), body.Config)
	c.Header("Cache-Control", "no-store")
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, value)
}
