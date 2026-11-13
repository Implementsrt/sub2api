package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
)

// Optional extension interface avoids changing upstream UsageLogRepository and
// its many implementations/mocks when syncing the fork.
type personalUsageContextReader interface {
	ListPersonalUsageContext(context.Context, int64, []UsageLog) ([]UsageLog, error)
}

func (s *UsageService) PersonalUsageDrops(ctx context.Context, userID int64, records []UsageLog) (map[int64]*personalusage.CacheDrop, error) {
	if len(records) == 0 {
		return map[int64]*personalusage.CacheDrop{}, nil
	}
	reader, ok := s.usageRepo.(personalUsageContextReader)
	if !ok {
		return nil, errors.New("personal usage context unavailable")
	}
	rows, err := reader.ListPersonalUsageContext(ctx, userID, records)
	if err != nil {
		return nil, err
	}
	identities, err := s.PersonalUsageIdentities(ctx, userID, rows)
	if err != nil {
		return nil, err
	}
	observations := make([]personalusage.Observation, 0, len(rows))
	for _, row := range rows {
		identity := identities[row.ID]
		session := identity.Scope()
		if session != "" {
			session = strconv.FormatInt(row.APIKeyID, 10) + ":" + session
		}
		// Guardian requests often lack a stable independent thread. They are
		// displayed but never participate in cache-drop comparisons.
		if identity != nil && strings.EqualFold(identity.Subagent, "guardian") {
			session = ""
		}
		// Compare the actual upstream model when mapped, falling back to the
		// recorded model for historical/native rows.
		model := row.Model
		if row.UpstreamModel != nil && strings.TrimSpace(*row.UpstreamModel) != "" {
			model = strings.TrimSpace(*row.UpstreamModel)
		}
		compact := row.NativeCompactionV2 || (identity != nil && identity.Kind == "compaction") || (row.InboundEndpoint != nil && strings.HasSuffix(*row.InboundEndpoint, "/responses/compact"))
		observations = append(observations, personalusage.Observation{
			ID: row.ID, UserID: row.UserID, SessionID: session, AccountID: row.AccountID,
			Model: model, CreatedAt: row.CreatedAt, DurationMs: row.DurationMs,
			InputTokens: row.InputTokens, CacheReadTokens: row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens, Compaction: compact,
		})
	}
	return personalusage.Detect(observations), nil
}
