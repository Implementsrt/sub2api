package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Installed once during dependency assembly, before the HTTP server starts.
func (r *usageLogRepository) SetUpstreamPricing(m *upstreampricing.Manager) {
	r.forkUpstreamPricing = m
}

func (r *usageLogRepository) RecordUpstreamPricing(row *service.UsageLog) {
	if r.forkUpstreamPricing == nil || row == nil || row.ForkUpstreamUsage == nil {
		return
	}
	r.forkUpstreamPricing.Submit(upstreampricing.UsageJob{ID: row.ID, RequestID: row.RequestID, APIKeyID: row.APIKeyID, UserID: row.UserID, Usage: *row.ForkUpstreamUsage})
}

func (r *usageLogRepository) ListUpstreamEstimates(ctx context.Context, userID int64, ids []int64) (map[int64]*upstreampricing.Estimate, error) {
	if r.forkUpstreamPricing == nil {
		return map[int64]*upstreampricing.Estimate{}, nil
	}
	return r.forkUpstreamPricing.Store.Estimates(ctx, userID, ids)
}
