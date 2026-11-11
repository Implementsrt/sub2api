package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamPricingHandler struct {
	pricing *service.UpstreamPricingService
}

func NewUpstreamPricingHandler(pricing *service.UpstreamPricingService) *UpstreamPricingHandler {
	return &UpstreamPricingHandler{pricing: pricing}
}

func (h *UpstreamPricingHandler) account(c *gin.Context) (int64, bool) {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.Error(c, 403, "上游定价仅供完整管理员管理")
		return 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "账号 ID 无效")
		return 0, false
	}
	if h.pricing == nil || h.pricing.Manager == nil {
		response.Error(c, 503, "上游定价服务未启用")
		return 0, false
	}
	if _, err = h.pricing.Store.AccountFingerprint(c.Request.Context(), id); err != nil {
		response.BadRequest(c, "账号不存在，或不是 API Key 账号")
		return 0, false
	}
	return id, true
}

func pricingError(c *gin.Context, err error) {
	// SQL/network diagnostics and remote response text must never reach the UI.
	message, status := upstreampricing.PublicMessage(err)
	response.Error(c, status, message)
}

func (h *UpstreamPricingHandler) Get(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	src, err := h.pricing.Store.Source(ctx, id)
	if err != nil {
		pricingError(c, err)
		return
	}
	runs, err := h.pricing.Store.History(ctx, id)
	if err != nil {
		pricingError(c, err)
		return
	}
	versions, err := h.pricing.Store.Versions(ctx, id)
	if err != nil {
		pricingError(c, err)
		return
	}
	type summary struct {
		ID            int64     `json:"id"`
		EffectiveFrom time.Time `json:"effective_from"`
		Supported     int       `json:"supported"`
		Unsupported   int       `json:"unsupported"`
	}
	metadata := []summary{}
	var active *upstreampricing.Catalog
	for _, v := range versions {
		metadata = append(metadata, summary{v.ID, v.EffectiveFrom, v.Catalog.Supported, v.Catalog.Unsupported})
		if src.ActiveVersion != nil && v.ID == *src.ActiveVersion {
			copy := v.Catalog
			active = &copy
		}
	}
	fp, _ := h.pricing.Store.AccountFingerprint(ctx, id)
	response.Success(c, gin.H{"source": src, "catalog": active, "runs": runs, "versions": metadata, "account_changed": src.HasCredential && fp != src.AccountFingerprint})
}

type pricingCandidate struct {
	Revision int64                  `json:"revision"`
	Config   upstreampricing.Config `json:"config"`
}

func (h *UpstreamPricingHandler) Groups(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	var body pricingCandidate
	if c.ShouldBindJSON(&body) != nil {
		response.BadRequest(c, "请求格式无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 50*time.Second)
	defer cancel()
	groups, err := h.pricing.Groups(ctx, id, body.Config)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, groups)
}
func (h *UpstreamPricingHandler) Preview(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	var body pricingCandidate
	if c.ShouldBindJSON(&body) != nil {
		response.BadRequest(c, "请求格式无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	preview, err := h.pricing.Preview(ctx, id, body.Revision, body.Config)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, preview)
}
func (h *UpstreamPricingHandler) Apply(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	var body struct {
		PreviewID string `json:"preview_id"`
	}
	if c.ShouldBindJSON(&body) != nil || len(body.PreviewID) != 48 {
		response.BadRequest(c, "预览 ID 无效")
		return
	}
	version, err := h.pricing.Store.ApplyPreview(c.Request.Context(), id, body.PreviewID)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, gin.H{"version_id": version})
}
func (h *UpstreamPricingHandler) Sync(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	version, err := h.pricing.Sync(ctx, id, "manual_sync")
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, gin.H{"version_id": version})
}
func (h *UpstreamPricingHandler) Schedule(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	var body struct {
		Automatic       bool `json:"automatic"`
		IntervalMinutes int  `json:"interval_minutes"`
	}
	if c.ShouldBindJSON(&body) != nil {
		response.BadRequest(c, "请求格式无效")
		return
	}
	if err := h.pricing.Store.Schedule(c.Request.Context(), id, body.Automatic, body.IntervalMinutes); err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *UpstreamPricingHandler) RollbackPreview(c *gin.Context) {
	id, ok := h.account(c)
	if !ok {
		return
	}
	var body struct {
		VersionID int64 `json:"version_id"`
	}
	if c.ShouldBindJSON(&body) != nil || body.VersionID <= 0 {
		response.BadRequest(c, "版本 ID 无效")
		return
	}
	p, err := h.pricing.RollbackPreview(c.Request.Context(), id, body.VersionID)
	if err != nil {
		pricingError(c, err)
		return
	}
	response.Success(c, p)
}
