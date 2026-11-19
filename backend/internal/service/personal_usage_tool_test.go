//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/stretchr/testify/require"
)

func TestPersonalUsageToolIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body, session, thread string
		headers                     map[string]string
	}{
		{"search metadata", `{"id":"fallback"}`, "child", "child", map[string]string{openAIWSTurnMetadataHeader: `{"thread_id":"child","request_kind":"turn"}`}},
		{"preserve correlation", `{"id":"fallback"}`, "parent", "child", map[string]string{"session_id": "parent", "thread-id": "child"}},
		{"body metadata", `{"id":"fallback","client_metadata":{"thread_id":"child"}}`, "child", "child", nil},
		{"body correlation only", `{"id":"parent"}`, "parent", "", nil},
		{"no identifier", `{}`, "", "", nil},
		{"invalid correlation", `{"id":"bad\nidentity"}`, "", "", nil},
		{"non-string correlation", `{"id":123}`, "", "", nil},
		{"no content inference", `{"input":[{"thread_id":"untrusted"}]}`, "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, identity := ExtractPersonalUsageToolIdentity(newSessionHeaderContext(t, tc.headers), []byte(tc.body))
			require.Equal(t, tc.session, session)
			require.Equal(t, tc.thread, identity.ThreadID)
			require.Equal(t, "tool", identity.Kind)
			require.Empty(t, identity.Scope(), "tool requests must never enter the cache-comparison index")
		})
	}
}

func TestPersonalUsageToolDisplay(t *testing.T) {
	parent := "parent"
	for _, endpoint := range []string{"/v1/alpha/search", "/alpha/search", "/backend-api/codex/alpha/search/"} {
		row := UsageLog{SessionID: &parent, InboundEndpoint: &endpoint}
		require.Equal(t, parent, PersonalUsageTitleID(row, nil))
		display := PersonalUsageDisplayIdentity(row, nil, personalusage.SessionInfo{})
		require.Equal(t, "tool", display.Kind)
		require.Equal(t, "related_session", display.Match)
		require.Empty(t, display.ThreadID, "a correlation ID must not be presented as an execution thread")
		require.Equal(t, "not_comparable", PersonalUsageMonitoringStatus(row, nil, "ready"))
		require.Equal(t, "not_comparable", PersonalUsageMonitoringStatus(row, nil, "unavailable"))
		child := &personalusage.Identity{ThreadID: "child", Subagent: "guardian"}
		require.Equal(t, "child", PersonalUsageTitleID(row, child))
		display = PersonalUsageDisplayIdentity(row, child, personalusage.SessionInfo{ParentThreadID: parent})
		require.Equal(t, "thread", display.Match)
		require.Equal(t, "child", display.ThreadID)
		require.Equal(t, parent, display.ParentThreadID)
		require.Equal(t, "tool", display.Kind)
	}
	// A synchronous model invocation is not necessarily a tool call.
	model := UsageLog{SessionID: &parent, RequestType: RequestTypeSync}
	require.False(t, IsPersonalUsageTool(model, nil))
	require.Equal(t, "identity_missing", PersonalUsageMonitoringStatus(model, nil, "ready"))
}

func TestPersonalUsageToolRecording(t *testing.T) {
	r := &executionCaptureRepo{}
	svc := newOpenAIRecordUsageServiceForTest(r, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.cfg.RunMode = config.RunModeSimple
	session, identity := ExtractPersonalUsageToolIdentity(newSessionHeaderContext(t, map[string]string{openAIWSTurnMetadataHeader: `{"thread_id":"child"}`}), nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "tool-request", Model: "gpt-5.1", WebSearchCalls: 1, Duration: time.Second},
		APIKey: &APIKey{ID: 8}, User: &User{ID: 42}, Account: &Account{ID: 7}, SessionID: session, ForkIdentity: identity,
	})
	require.NoError(t, err)
	require.True(t, r.savedFirst)
	require.Equal(t, "child", *r.lastLog.SessionID)
	require.Equal(t, identity, r.identity)
	require.Empty(t, r.identity.Scope())
}

func TestPersonalUsageToolExcludedBetweenTurns(t *testing.T) {
	duration := 100
	base := UsageLog{ID: 1, UserID: 42, APIKeyID: 8, AccountID: 7, Model: "gpt", InputTokens: 10, CacheReadTokens: 90, CreatedAt: time.Now(), DurationMs: &duration}
	tool, after := base, base
	tool.ID, tool.CreatedAt = 2, base.CreatedAt.Add(time.Minute)
	after.ID, after.CreatedAt, after.InputTokens, after.CacheReadTokens = 3, base.CreatedAt.Add(2*time.Minute), 80, 20
	endpoint := "/v1/alpha/search"
	tool.InboundEndpoint = &endpoint
	for _, kind := range []string{"tool", "turn"} { // Defensive support for old sidecars.
		r := &executionUsageRepo{rows: []UsageLog{base, tool, after}, identities: map[int64]*personalusage.Identity{
			1: {ThreadID: "child"}, 2: {ThreadID: "child", Kind: kind}, 3: {ThreadID: "child"},
		}}
		drops, err := NewUsageService(r, nil, nil, nil).PersonalUsageDrops(context.Background(), 42, []UsageLog{tool, after})
		require.NoError(t, err)
		require.NotContains(t, drops, int64(2))
		require.NotNil(t, drops[3], "a tool must not interrupt comparison between actual model turns")
		require.Equal(t, int64(1), drops[3].PreviousRequestID)
	}
}
