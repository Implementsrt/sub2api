package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"

	entsql "entgo.io/ent/dialect/sql"
)

type activeNewAPIGroup struct {
	group       upstreampricing.Group
	fingerprint string
}

// A nil ID list is used when sorting needs the rates for all accounts before pagination.
func (r *accountRepository) activeNewAPIGroups(ctx context.Context, ids []int64) (map[int64]activeNewAPIGroup, error) {
	if r.sql == nil || adminaccess.RestrictedFromContext(ctx) != nil || (ids != nil && len(ids) == 0) {
		return nil, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT a.id, a.platform, a.credentials, s.account_fingerprint, v.catalog -> 'group'
		FROM fork_upstream_pricing_sources s
		JOIN fork_upstream_pricing_versions v ON v.id = s.active_version AND v.account_id = s.account_id
			AND v.account_fingerprint = s.account_fingerprint
		JOIN accounts a ON a.id = s.account_id AND a.deleted_at IS NULL AND a.type = 'apikey'
		WHERE s.config ->> 'provider' = 'newapi' AND ($1::bigint[] IS NULL OR a.id = ANY($1))
	`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make(map[int64]activeNewAPIGroup)
	for rows.Next() {
		var id int64
		var platform, fingerprint string
		var rawCredentials, rawGroup []byte
		if err := rows.Scan(&id, &platform, &rawCredentials, &fingerprint, &rawGroup); err != nil {
			return nil, err
		}
		var credentials map[string]any
		var group upstreampricing.Group
		if json.Unmarshal(rawCredentials, &credentials) == nil &&
			fingerprint == upstreampricing.AccountFingerprint(service.AccountTypeAPIKey, platform, credentials) &&
			json.Unmarshal(rawGroup, &group) == nil && group.Name != "" && group.Multiplier != "" {
			groups[id] = activeNewAPIGroup{group, fingerprint}
		}
	}
	return groups, rows.Err()
}

func assignNewAPIGroups(accounts []service.Account, groups map[int64]activeNewAPIGroup) {
	for i := range accounts {
		account := &accounts[i]
		group, ok := groups[account.ID]
		if ok && group.fingerprint == upstreampricing.AccountFingerprint(account.Type, account.Platform, account.Credentials) {
			account.NewAPIGroupRate = &group.group
		}
	}
}

func orderByUpstreamBillingRate(s *entsql.Selector, groups map[int64]activeNewAPIGroup, legacyRate, direction, id string) {
	if len(groups) == 0 {
		s.OrderExpr(entsql.Expr(legacyRate + " " + direction + " NULLS LAST"))
		return
	}
	multipliers := make(map[int64]string, len(groups))
	for accountID, group := range groups {
		multipliers[accountID] = group.group.Multiplier
	}
	payload, _ := json.Marshal(multipliers)
	s.OrderExpr(entsql.ExprFunc(func(b *entsql.Builder) {
		b.WriteString("COALESCE((").Arg(string(payload)).WriteString("::jsonb ->> ")
		b.WriteString(id).WriteString("::text)::numeric, ").WriteString(legacyRate)
		b.WriteString(") ").WriteString(direction).WriteString(" NULLS LAST")
	}))
}
