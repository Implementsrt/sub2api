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

func TestExtractPersonalUsageIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		headers                      map[string]string
		body, thread, kind, subagent string
	}{
		{"child header", map[string]string{"session_id": "parent", "thread-id": "child"}, "", "child", "", ""},
		{"turn metadata", map[string]string{"session_id": "parent", openAIWSTurnMetadataHeader: `{"thread_id":"child","request_kind":"turn"}`}, "", "child", "turn", ""},
		{"HTTP body metadata", map[string]string{"session_id": "parent"}, `{"client_metadata":{"thread_id":"child","x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`, "child", "memory", ""},
		{"embedded turn metadata", nil, `{"client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"child\",\"request_kind\":\"turn\",\"subagent_kind\":\"guardian\"}"}}`, "child", "turn", "guardian"},
		{"guardian without thread", map[string]string{"session_id": "parent", openAISubagentHeader: "guardian"}, "", "", "", "guardian"},
		{"header precedence", map[string]string{"thread-id": "header-child", openAIWSTurnMetadataHeader: `{"thread_id":"metadata-child","request_kind":"turn"}`}, `{"client_metadata":{"thread_id":"body-child"}}`, "header-child", "turn", ""},
		{"window is not execution identity", map[string]string{"session_id": "parent", openAIWSWindowIDHeader: "parent:window"}, "", "", "", ""},
		{"do not infer from content", nil, `{"prompt_cache_key":"cache","input":[{"thread_id":"untrusted-text"}]}`, "", "", ""},
		{"invalid identity", map[string]string{"thread-id": "bad\nidentity"}, "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newSessionHeaderContext(t, tc.headers)
			i := ExtractPersonalUsageIdentity(c, []byte(tc.body))
			require.Equal(t, tc.thread, i.ThreadID)
			require.Equal(t, tc.kind, i.Kind)
			require.Equal(t, tc.subagent, i.Subagent)
			// The raw original session remains unchanged for existing correlation.
			if tc.headers["session_id"] != "" {
				require.Equal(t, tc.headers["session_id"], ExtractClientSessionID(c))
			}
		})
	}
}

type executionCaptureRepo struct {
	openAIRecordUsageLogRepoStub
	identity   *personalusage.Identity
	savedFirst bool
}

func (r *executionCaptureRepo) RecordPersonalUsageIdentity(_ context.Context, row *UsageLog) error {
	r.identity = row.ForkIdentity
	r.savedFirst = r.calls > 0
	return nil
}

func TestPersonalUsageIdentitySurvivesRecording(t *testing.T) {
	r := &executionCaptureRepo{}
	svc := newOpenAIRecordUsageServiceForTest(r, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.cfg.RunMode = config.RunModeSimple
	identity := &personalusage.Identity{ThreadID: "child", Kind: "turn", Source: "client_metadata_v1"}
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "synthetic-request", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}, Duration: time.Second},
		APIKey: &APIKey{ID: 8}, User: &User{ID: 42}, Account: &Account{ID: 7}, SessionID: "original-parent", ForkIdentity: identity,
	})
	require.NoError(t, err)
	require.True(t, r.savedFirst, "sidecar recording must follow usage persistence")
	require.Equal(t, identity, r.identity)
	require.Equal(t, "original-parent", *r.lastLog.SessionID)
}

func TestPersonalUsageUnresolvedReviewTitle(t *testing.T) {
	parent := "parent-window"
	row := UsageLog{SessionID: &parent}
	require.Empty(t, PersonalUsageTitleID(row, &personalusage.Identity{Subagent: "guardian"}), "review must not borrow its parent window's title")
	require.Equal(t, "child", PersonalUsageTitleID(row, &personalusage.Identity{ThreadID: "child"}))
	require.Equal(t, parent, PersonalUsageTitleID(row, nil), "legacy title remains available with the explicit session-only marker")
}

type executionUsageRepo struct {
	UsageLogRepository
	rows       []UsageLog
	identities map[int64]*personalusage.Identity
}

func (r *executionUsageRepo) ListPersonalUsageContext(context.Context, int64, []UsageLog) ([]UsageLog, error) {
	return r.rows, nil
}
func (r *executionUsageRepo) ListPersonalUsageIdentities(context.Context, int64, []UsageLog) (map[int64]*personalusage.Identity, error) {
	return r.identities, nil
}

func TestPersonalUsageExecutionIsolation(t *testing.T) {
	duration := 100
	base := UsageLog{ID: 1, UserID: 42, APIKeyID: 8, AccountID: 7, Model: "gpt", InputTokens: 10, CacheReadTokens: 90, CreatedAt: time.Now(), DurationMs: &duration}
	after := base
	after.ID = 2
	after.CreatedAt = base.CreatedAt.Add(time.Minute)
	after.InputTokens = 80
	after.CacheReadTokens = 20
	for _, tc := range []struct {
		name           string
		before, after  *personalusage.Identity
		otherKey, drop bool
	}{
		{"same child", &personalusage.Identity{ThreadID: "child"}, &personalusage.Identity{ThreadID: "child"}, false, true},
		{"parent vs child", &personalusage.Identity{ThreadID: "parent"}, &personalusage.Identity{ThreadID: "child"}, false, false},
		{"sibling", &personalusage.Identity{ThreadID: "child-a"}, &personalusage.Identity{ThreadID: "child-b"}, false, false},
		{"memory vs turn", &personalusage.Identity{ThreadID: "parent", Kind: "memory"}, &personalusage.Identity{ThreadID: "parent", Kind: "turn"}, false, false},
		{"guardian", &personalusage.Identity{ThreadID: "parent", Subagent: "guardian"}, &personalusage.Identity{ThreadID: "parent", Subagent: "guardian"}, false, false},
		{"legacy", nil, nil, false, false},
		{"missing thread", &personalusage.Identity{}, &personalusage.Identity{}, false, false},
		{"different key", &personalusage.Identity{ThreadID: "child"}, &personalusage.Identity{ThreadID: "child"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := base, after
			if tc.otherKey {
				b.APIKeyID++
			}
			r := &executionUsageRepo{rows: []UsageLog{a, b}, identities: map[int64]*personalusage.Identity{1: tc.before, 2: tc.after}}
			drops, err := NewUsageService(r, nil, nil, nil).PersonalUsageDrops(context.Background(), 42, []UsageLog{b})
			require.NoError(t, err)
			require.Equal(t, tc.drop, drops[2] != nil)
		})
	}
}
