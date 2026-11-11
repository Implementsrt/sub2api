package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
)

type UpstreamPricingService struct{ *upstreampricing.Manager }

func NewUpstreamPricingService(db *sql.DB, cfg *config.Config, cipher SecretEncryptor, repo UsageLogRepository) *UpstreamPricingService {
	m := upstreampricing.NewManager(&upstreampricing.Store{DB: db}, upstreampricing.NewRemote(cfg.Update.ProxyURL), cipher)
	if extension, ok := repo.(interface {
		SetUpstreamPricing(*upstreampricing.Manager)
	}); ok {
		extension.SetUpstreamPricing(m)
	}
	m.Start()
	return &UpstreamPricingService{Manager: m}
}

func submitUpstreamPricing(repo UsageLogRepository, row *UsageLog) {
	if row == nil || row.ForkUpstreamUsage == nil {
		return
	}
	if extension, ok := repo.(interface{ RecordUpstreamPricing(*UsageLog) }); ok {
		extension.RecordUpstreamPricing(row)
	}
}

func (s *UsageService) UpstreamEstimates(ctx context.Context, userID int64, records []UsageLog) (map[int64]*upstreampricing.Estimate, error) {
	ids := make([]int64, 0, len(records))
	for _, r := range records {
		if userID == 0 || r.UserID == userID {
			ids = append(ids, r.ID)
		}
	}
	if reader, ok := s.usageRepo.(interface {
		ListUpstreamEstimates(context.Context, int64, []int64) (map[int64]*upstreampricing.Estimate, error)
	}); ok {
		return reader.ListUpstreamEstimates(ctx, userID, ids)
	}
	return map[int64]*upstreampricing.Estimate{}, nil
}

func pricingUsageBase(account *Account, model, upstream string, at time.Time, tier *string) *upstreampricing.Usage {
	if account == nil || account.Type != "apikey" {
		return nil
	}
	if upstream != "" {
		model = upstream
	}
	u := &upstreampricing.Usage{AccountID: account.ID, AccountFingerprint: upstreampricing.AccountFingerprint(account.Type, account.Platform, account.Credentials), Model: model, PricingAt: at}
	if tier != nil {
		u.ServiceTier = strings.TrimSpace(*tier)
	}
	return u
}

// Capture before local ForceCacheBilling and TTL billing overrides mutate usage.
func captureClaudeUpstreamUsage(input *recordUsageCoreInput) *upstreampricing.Usage {
	r := input.Result
	u := pricingUsageBase(input.Account, r.Model, r.UpstreamModel, input.PricingAt, r.ServiceTier)
	if u == nil {
		return nil
	}
	u.Input = r.Usage.InputTokens
	u.Output = r.Usage.OutputTokens
	u.CacheRead = r.Usage.CacheReadInputTokens
	u.CacheWrite = r.Usage.CacheCreationInputTokens
	u.CacheWrite5m = r.Usage.CacheCreation5mTokens
	u.CacheWrite1h = r.Usage.CacheCreation1hTokens
	if r.UpstreamResponseServiceTier != "" {
		u.ServiceTier = r.UpstreamResponseServiceTier
	}
	u.ExtraUnits = r.ImageCount > 0 || r.Usage.ImageOutputTokens > 0 || r.SearchCount > 0 || r.AudioUsage != nil || r.CacheStorageTokenHours > 0
	return u
}

func captureOpenAIUpstreamUsage(input *OpenAIRecordUsageInput) *upstreampricing.Usage {
	r := input.Result
	u := pricingUsageBase(input.Account, r.Model, r.UpstreamModel, input.PricingAt, r.ServiceTier)
	if u == nil {
		return nil
	}
	u.Input = r.Usage.InputTokens - r.Usage.CacheReadInputTokens - r.Usage.CacheCreationInputTokens
	u.Output = r.Usage.OutputTokens
	u.CacheRead = r.Usage.CacheReadInputTokens
	u.CacheWrite = r.Usage.CacheCreationInputTokens
	u.CacheWrite30m = r.Usage.CacheCreation30mInputTokens
	u.CacheWrite12h = r.Usage.CacheCreation12hInputTokens
	if r.UpstreamResponseServiceTier != "" {
		u.ServiceTier = r.UpstreamResponseServiceTier
	}
	u.ExtraUnits = r.ImageCount > 0 || r.VideoCount > 0 || r.Usage.ImageInputTokens > 0 || r.Usage.ImageOutputTokens > 0 || r.WebSearchCalls > 0 || r.SearchCount > 0 || r.AudioUsage != nil
	return u
}
