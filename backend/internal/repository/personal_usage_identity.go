package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageLogRepository) RecordPersonalUsageIdentity(ctx context.Context, row *service.UsageLog) error {
	identity := row.ForkIdentity
	if identity == nil {
		return nil
	}
	// Best-effort batching does not populate row.ID. Resolve the persisted row
	// by its existing unique (request_id, api_key_id), also binding its owner.
	_, err := r.sql.ExecContext(ctx, `INSERT INTO fork_usage_execution_identity
		(usage_id, user_id, api_key_id, thread_id, request_kind, subagent, source, scope_key, created_at)
		SELECT id, user_id, api_key_id, $4, $5, $6, $7, $8, created_at FROM usage_logs
		WHERE request_id = $1 AND api_key_id = $2 AND user_id = $3
		ON CONFLICT (usage_id) DO NOTHING`, row.RequestID, row.APIKeyID, row.UserID,
		identity.ThreadID, identity.Kind, identity.Subagent, identity.Source, identity.Scope())
	return err
}

func (r *usageLogRepository) ListPersonalUsageIdentities(ctx context.Context, userID int64, records []service.UsageLog) (map[int64]*personalusage.Identity, error) {
	out := map[int64]*personalusage.Identity{}
	args := []any{userID}
	ids := []string{}
	for _, row := range records {
		if userID != 0 && row.UserID != userID {
			continue
		}
		args = append(args, row.ID)
		ids = append(ids, fmt.Sprintf("$%d", len(args)))
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT usage_id, thread_id, request_kind, subagent, source
		FROM fork_usage_execution_identity WHERE ($1::bigint = 0 OR user_id = $1) AND usage_id IN (`+strings.Join(ids, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var identity personalusage.Identity
		if err := rows.Scan(&id, &identity.ThreadID, &identity.Kind, &identity.Subagent, &identity.Source); err != nil {
			return nil, err
		}
		out[id] = &identity
	}
	return out, rows.Err()
}
