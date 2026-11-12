package upstreampricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type ComparisonAccount struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Provider       string     `json:"provider"`
	BaseURL        string     `json:"base_url"`
	Group          string     `json:"group"`
	Automatic      bool       `json:"automatic"`
	VersionID      *int64     `json:"version_id"`
	LastSuccess    *time.Time `json:"last_success"`
	NextSync       *time.Time `json:"next_sync"`
	Stale          bool       `json:"stale"`
	AccountChanged bool       `json:"account_changed"`
	LastError      string     `json:"last_error"`
}

type ComparisonPrice struct {
	AccountID  int64  `json:"account_id"`
	VersionID  int64  `json:"version_id"`
	Model      string `json:"model"`
	Multiplier string `json:"multiplier"`
	Base       Price  `json:"base"`
	Effective  Price  `json:"effective"`
}

type Comparison struct {
	Accounts []ComparisonAccount `json:"accounts"`
	Items    []ComparisonPrice   `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func effectivePrice(p Price, multiplier string) (Price, error) {
	if p.Mode == "unavailable" {
		return p, nil
	}
	ratio, err := decimal(multiplier)
	if err != nil {
		return Price{}, err
	}
	copy := p
	fields := []**string{&copy.Input, &copy.Output, &copy.CacheRead, &copy.CacheWrite, &copy.CacheWrite1h, &copy.CacheWrite30m, &copy.CacheWrite12h, &copy.Request}
	for _, field := range fields {
		if *field == nil {
			continue
		}
		value, e := decimal(**field)
		if e != nil {
			return Price{}, e
		}
		value.Mul(value, ratio)
		text := format(value, 36) // Input decimals each have at most 18 places.
		*field = &text
	}
	return copy, nil
}

// Only saved active snapshots are read. This operation never calls a station,
// writes a new catalog, or exposes account/panel credentials.
func (s *Store) Comparison(ctx context.Context, search string, accountID int64, page, pageSize int) (*Comparison, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &Comparison{Accounts: []ComparisonAccount{}, Items: []ComparisonPrice{}, Page: page, PageSize: pageSize}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.name,a.type,a.platform,a.credentials,s.config,s.account_fingerprint,
 s.active_version,s.last_success,s.next_sync,COALESCE(s.last_error,'')
 FROM accounts a LEFT JOIN fork_upstream_pricing_sources s ON s.account_id=a.id
 WHERE a.deleted_at IS NULL AND a.type='apikey' ORDER BY a.id LIMIT 501`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a ComparisonAccount
		var kind, platform string
		var credentials, config []byte
		var savedFP sql.NullString
		if err = rows.Scan(&a.ID, &a.Name, &kind, &platform, &credentials, &config, &savedFP, &a.VersionID, &a.LastSuccess, &a.NextSync, &a.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		var c Config
		var private map[string]any
		if len(config) > 0 && json.Unmarshal(config, &c) != nil {
			rows.Close()
			return nil, safeError("已保存的定价配置不可读")
		}
		if json.Unmarshal(credentials, &private) != nil {
			rows.Close()
			return nil, safeError("上游账号配置不可读")
		}
		a.Provider, a.BaseURL, a.Group, a.Automatic = c.Provider, c.BaseURL, c.Group, c.Automatic
		a.AccountChanged = savedFP.Valid && savedFP.String != AccountFingerprint(kind, platform, private)
		a.Stale = a.LastSuccess != nil && time.Since(*a.LastSuccess) > time.Duration(max(60, c.IntervalMinutes)*2)*time.Minute
		out.Accounts = append(out.Accounts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Accounts) > 500 {
		return nil, safeError("上游账号数量超过对比页上限")
	}
	const from = ` FROM accounts a JOIN fork_upstream_pricing_sources s ON s.account_id=a.id
 JOIN fork_upstream_pricing_versions v ON v.id=s.active_version AND v.account_id=a.id
 CROSS JOIN LATERAL jsonb_array_elements(v.catalog->'models') p
 WHERE a.deleted_at IS NULL AND a.type='apikey' AND ($1::bigint=0 OR a.id=$1)
 AND strpos(lower(p->>'model'),lower($2))>0`
	if err = tx.QueryRowContext(ctx, `SELECT count(*)`+from, accountID, search).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.id,v.id,p,v.catalog->'group'->>'multiplier'`+from+` ORDER BY p->>'model',a.id LIMIT $3 OFFSET $4`, accountID, search, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item ComparisonPrice
		var price []byte
		if err = rows.Scan(&item.AccountID, &item.VersionID, &price, &item.Multiplier); err != nil {
			rows.Close()
			return nil, err
		}
		if json.Unmarshal(price, &item.Base) != nil {
			rows.Close()
			return nil, safeError("已同步的价格快照不可读")
		}
		item.Model = item.Base.Model
		item.Effective, err = effectivePrice(item.Base, item.Multiplier)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
