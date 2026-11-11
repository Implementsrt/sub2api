package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamPricingCapturePreservesRawUsage(t *testing.T) {
	account := &Account{ID: 7, Type: "apikey", Platform: "anthropic", Credentials: map[string]any{"base_url": "https://example.com", "api_key": "synthetic-key"}}
	start := time.Now()
	result := &ForwardResult{Model: "client", UpstreamModel: "actual", Usage: ClaudeUsage{InputTokens: 50, OutputTokens: 10, CacheReadInputTokens: 100, CacheCreationInputTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 20}}
	u := captureClaudeUpstreamUsage(&recordUsageCoreInput{Account: account, Result: result, PricingAt: start})
	require.NotNil(t, u)
	result.Usage.InputTokens = 0
	result.Usage.CacheReadInputTokens = 150
	result.Usage.CacheCreation1hTokens = 0
	require.Equal(t, 50, u.Input)
	require.Equal(t, 100, u.CacheRead)
	require.Equal(t, 20, u.CacheWrite1h)
	require.Equal(t, "actual", u.Model)
	require.Equal(t, start, u.PricingAt)
	ai := &OpenAIForwardResult{Model: "client", UpstreamModel: "actual", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 10, CacheReadInputTokens: 700, CacheCreationInputTokens: 100, CacheCreation30mInputTokens: 60, CacheCreation12hInputTokens: 40}}
	open := captureOpenAIUpstreamUsage(&OpenAIRecordUsageInput{Account: account, Result: ai, PricingAt: start})
	require.Equal(t, 200, open.Input)
	require.Equal(t, 700, open.CacheRead)
	require.Equal(t, 60, open.CacheWrite30m)
	require.Equal(t, 40, open.CacheWrite12h)
	account.Type = "oauth"
	require.Nil(t, captureOpenAIUpstreamUsage(&OpenAIRecordUsageInput{Account: account, Result: ai}))
}
