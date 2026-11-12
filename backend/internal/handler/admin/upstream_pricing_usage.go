package admin

import (
	"context"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var adminUsageTitles = personalusage.NewTitleResolver()

func (h *UsageHandler) upstreamPricingRows(c *gin.Context, records []service.UsageLog) any {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		return adminUsageLogResponses(c, records)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	estimates, _ := h.usageService.UpstreamEstimates(ctx, 0, records)
	// Keep predecessor queries scoped to each record's owner, including when
	// an administrator views several users on one page.
	byUser := make(map[int64][]service.UsageLog)
	for _, record := range records {
		byUser[record.UserID] = append(byUser[record.UserID], record)
	}
	drops := make(map[int64]*personalusage.CacheDrop)
	statuses := make(map[int64]string)
	dropCtx, dropCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer dropCancel()
	for userID, rows := range byUser {
		userDrops, err := h.usageService.PersonalUsageDrops(dropCtx, userID, rows)
		statuses[userID] = "ready"
		if err != nil {
			statuses[userID] = "unavailable"
		}
		for id, drop := range userDrops {
			drops[id] = drop
		}
	}
	// Local names belong to the operator; other users' session IDs do not
	// authorize reading the operator's desktop metadata.
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	titles := map[string]string{}
	if authenticated && os.Getenv("PERSONAL_USAGE_LOCAL_TITLES") != "false" {
		ids := make([]string, 0, len(records))
		for _, record := range records {
			if record.UserID == subject.UserID && record.SessionID != nil {
				ids = append(ids, *record.SessionID)
			}
		}
		titleCtx, titleCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		titles = adminUsageTitles.Resolve(titleCtx, ids)
		titleCancel()
	}
	type row struct {
		dto.AdminUsageLog
		SessionName           string                    `json:"session_name,omitempty"`
		CacheDrop             *personalusage.CacheDrop  `json:"cache_drop,omitempty"`
		CacheMonitoringStatus string                    `json:"cache_monitoring_status"`
		UpstreamEstimate      *upstreampricing.Estimate `json:"upstream_estimate,omitempty"`
	}
	out := make([]row, 0, len(records))
	for i := range records {
		record := &records[i]
		item := row{AdminUsageLog: *dto.UsageLogFromServiceAdmin(record), CacheDrop: drops[record.ID], CacheMonitoringStatus: statuses[record.UserID], UpstreamEstimate: estimates[record.ID]}
		if authenticated && record.UserID == subject.UserID && record.SessionID != nil {
			item.SessionName = titles[*record.SessionID]
		}
		out = append(out, item)
	}
	return out
}
