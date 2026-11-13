package handler

import (
	"context"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var personalUsageTitles = personalusage.NewTitleResolver()

// Embedding the upstream DTO keeps the fork additions out of generated models
// and shared admin/public-key responses.
type personalUsageRow struct {
	dto.UsageLog
	SessionIdentity       personalusage.IdentityDisplay `json:"session_identity"`
	SessionName           string                        `json:"session_name,omitempty"`
	CacheDrop             *personalusage.CacheDrop      `json:"cache_drop,omitempty"`
	CacheMonitoringStatus string                        `json:"cache_monitoring_status"`
	UpstreamEstimate      *upstreampricing.Estimate     `json:"upstream_estimate,omitempty"`
}

func (h *UsageHandler) personalUsageRows(c *gin.Context, records []service.UsageLog) []personalUsageRow {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	drops, err := h.usageService.PersonalUsageDrops(ctx, subject.UserID, records)
	cancel()
	status := "ready"
	if err != nil {
		status = "unavailable"
	}
	titles := map[string]personalusage.SessionInfo{}
	identityCtx, identityCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	identities, identityErr := h.usageService.PersonalUsageIdentities(identityCtx, subject.UserID, records)
	identityCancel()
	if identityErr != nil {
		status = "unavailable"
	}
	role, _ := middleware.GetUserRoleFromContext(c)
	estimates := map[int64]*upstreampricing.Estimate{}
	if role == service.RoleAdmin {
		estimateCtx, estimateCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		estimates, _ = h.usageService.UpstreamEstimates(estimateCtx, subject.UserID, records)
		estimateCancel()
	}
	// Local desktop titles belong to the operator, not arbitrary platform
	// users. A caller's session header alone does not grant title access.
	if role == service.RoleAdmin && os.Getenv("PERSONAL_USAGE_LOCAL_TITLES") != "false" {
		ids := make([]string, 0, len(records))
		for _, row := range records {
			ids = append(ids, service.PersonalUsageTitleID(row, identities[row.ID]))
		}
		titleCtx, titleCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		titles = personalUsageTitles.ResolveDetails(titleCtx, ids)
		titleCancel()
	}
	out := make([]personalUsageRow, 0, len(records))
	for i := range records {
		row := personalUsageRow{UsageLog: *dto.UsageLogFromService(&records[i]).Localized(c.GetHeader("Accept-Language")), CacheDrop: drops[records[i].ID], CacheMonitoringStatus: status}
		row.UpstreamEstimate = estimates[records[i].ID]
		info := titles[service.PersonalUsageTitleID(records[i], identities[records[i].ID])]
		row.SessionName = info.Name
		row.SessionIdentity = personalusage.DisplayIdentity(identities[records[i].ID], info)
		row.CacheMonitoringStatus = service.PersonalUsageMonitoringStatus(records[i], identities[records[i].ID], status)
		if row.SessionIdentity.Subagent == "guardian" {
			row.CacheDrop = nil
			row.CacheMonitoringStatus = "not_comparable"
		}
		out = append(out, row)
	}
	return out
}
