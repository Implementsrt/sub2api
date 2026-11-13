package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListPersonalUsageContext looks across pages/filters, but never across users.
// Equal-time batches on either side are retained so Detect can reject ambiguous
// ordering instead of choosing an arbitrary request by ID.
func (r *usageLogRepository) ListPersonalUsageContext(ctx context.Context, userID int64, records []service.UsageLog) ([]service.UsageLog, error) {
	args := []any{userID}
	ids := make([]string, 0, len(records))
	for _, row := range records {
		if row.UserID != userID {
			continue
		}
		args = append(args, row.ID)
		ids = append(ids, fmt.Sprintf("$%d", len(args)))
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return r.queryUsageLogs(ctx, personalUsageContextQuery(ids, usageLogSelectColumns), args...)
}

func personalUsageContextQuery(ids []string, columns string) string {
	return `WITH targets AS (
        SELECT usage_id, api_key_id, scope_key, created_at FROM fork_usage_execution_identity
        WHERE user_id = $1 AND scope_key <> '' AND usage_id IN (` + strings.Join(ids, ",") + `)
    ), neighbors AS (
        SELECT neighbor.usage_id FROM targets t CROSS JOIN LATERAL (
            (SELECT usage_id FROM fork_usage_execution_identity
             WHERE user_id = $1 AND api_key_id = t.api_key_id AND scope_key = t.scope_key AND created_at = t.created_at)
            UNION ALL
            (SELECT usage_id FROM fork_usage_execution_identity
             WHERE user_id = $1 AND api_key_id = t.api_key_id AND scope_key = t.scope_key
             AND created_at < t.created_at AND created_at >= t.created_at - INTERVAL '5 minutes'
             ORDER BY created_at DESC FETCH FIRST 1 ROW WITH TIES)
        ) neighbor
    )
    SELECT ` + columns + ` FROM usage_logs
    WHERE user_id = $1 AND id IN (SELECT usage_id FROM neighbors)`
}
