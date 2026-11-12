package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func browserOwner(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, 403, "需要管理员登录")
		return 0, false
	}
	return subject.UserID, true
}
func (h *UpstreamPricingHandler) StartBrowserImport(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	owner, ok := browserOwner(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var body struct {
		Revision int64                  `json:"revision"`
		Config   upstreampricing.Config `json:"config"`
		Origin   string                 `json:"origin"`
	}
	if c.ShouldBindJSON(&body) != nil {
		response.BadRequest(c, "请求格式无效")
		return
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if err := upstreampricing.ValidateBrowserOrigin(body.Origin, c.Request.Host, scheme); err != nil {
		pricingError(c, err)
		return
	}
	value, err := h.pricing.StartBrowserImport(c.Request.Context(), owner, id, body.Revision, body.Origin, body.Config)
	c.Header("Cache-Control", "no-store")
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, value)
}
func (h *UpstreamPricingHandler) BrowserImportStatus(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	owner, ok := browserOwner(c)
	if !ok {
		return
	}
	value, err := h.pricing.Imports.Status(c.Param("importId"), owner, id)
	c.Header("Cache-Control", "no-store")
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, value)
}
func (h *UpstreamPricingHandler) BrowserImportReceive(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.pricing == nil || h.pricing.Manager == nil {
		response.Error(c, 503, "服务未启用")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var body struct {
		Ticket string `json:"ticket"`
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	if c.ShouldBindJSON(&body) != nil || len(body.Ticket) != 48 {
		response.BadRequest(c, "连接格式无效")
		return
	}
	origin := c.GetHeader("Origin")
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if err := upstreampricing.ValidateBrowserOrigin(origin, c.Request.Host, scheme); err != nil {
		pricingError(c, err)
		return
	}
	if c.Param("action") == "info" {
		site, err := h.pricing.BrowserImportInfo(body.Ticket, origin)
		if err != nil {
			pricingError(c, err)
			return
		}
		response.Success(c, site)
		return
	}
	if c.Param("action") != "complete" {
		response.BadRequest(c, "连接操作无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 50*time.Second)
	defer cancel()
	if err := h.pricing.CompleteBrowserImport(ctx, body.Ticket, origin, body.Token, body.UserID); err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, gin.H{"verified": true})
}
