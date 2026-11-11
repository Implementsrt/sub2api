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
		if row.UserID != userID || row.SessionID == nil || *row.SessionID == "" {
			continue
		}
		args = append(args, row.ID)
		ids = append(ids, fmt.Sprintf("$%d", len(args)))
	}
	if len(ids) == 0 {
		return nil, nil
	}
	query := `WITH targets AS (
		SELECT id, session_id, created_at FROM usage_logs
		WHERE user_id = $1 AND id IN (` + strings.Join(ids, ",") + `)
	), neighbors AS (
		SELECT neighbor.id FROM targets t CROSS JOIN LATERAL (
			(SELECT id FROM usage_logs
			 WHERE user_id = $1 AND session_id = t.session_id AND created_at = t.created_at)
			UNION ALL
			(SELECT id FROM usage_logs
			 WHERE user_id = $1 AND session_id = t.session_id
			 AND created_at < t.created_at AND created_at >= t.created_at - INTERVAL '5 minutes'
			 ORDER BY created_at DESC FETCH FIRST 1 ROW WITH TIES)
		) neighbor
	)
	SELECT ` + usageLogSelectColumns + ` FROM usage_logs
	WHERE user_id = $1 AND id IN (SELECT id FROM neighbors)`
	return r.queryUsageLogs(ctx, query, args...)
}
