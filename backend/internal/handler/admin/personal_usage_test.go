package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminPersonalUsageRepo struct {
	service.UsageLogRepository
	rows  map[int64][]service.UsageLog
	users []int64
	fail  bool
}

func (r *adminPersonalUsageRepo) ListPersonalUsageContext(_ context.Context, userID int64, records []service.UsageLog) ([]service.UsageLog, error) {
	r.users = append(r.users, userID)
	for _, record := range records {
		if record.UserID != userID {
			return nil, errors.New("cross-user query")
		}
	}
	if r.fail {
		return nil, errors.New("unavailable")
	}
	return r.rows[userID], nil
}

func (r *adminPersonalUsageRepo) ListUpstreamEstimates(context.Context, int64, []int64) (map[int64]*upstreampricing.Estimate, error) {
	return nil, nil
}

func TestAdminPersonalUsageRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	id := "01a1c88f-8e4e-78d1-852e-e9f46ffcd6c6"
	path := filepath.Join(home, "session_index.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"`+id+`","thread_name":"Current name"}`), 0600))
	saved := adminUsageTitles
	adminUsageTitles = &personalusage.TitleResolver{CodexHome: home, ClaudeHome: home}
	t.Cleanup(func() { adminUsageTitles = saved })
	t.Setenv("PERSONAL_USAGE_LOCAL_TITLES", "true")
	duration := 1000
	before := service.UsageLog{ID: 10, UserID: 42, SessionID: &id, AccountID: 1, Model: "gpt", InputTokens: 10, CacheReadTokens: 90, DurationMs: &duration, CreatedAt: time.Now().Add(-time.Minute)}
	after := before
	after.ID, after.CreatedAt, after.InputTokens, after.CacheReadTokens = 11, before.CreatedAt.Add(time.Minute), 40, 60
	other := after
	other.ID, other.UserID = 12, 99
	repo := &adminPersonalUsageRepo{rows: map[int64][]service.UsageLog{42: {before, after}, 99: {other}}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/usage", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	decode := func() []map[string]any {
		body, err := json.Marshal(h.upstreamPricingRows(c, []service.UsageLog{after, other}))
		require.NoError(t, err)
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(body, &rows))
		return rows
	}
	rows := decode()
	require.ElementsMatch(t, []int64{42, 99}, repo.users)
	require.Equal(t, "Current name", rows[0]["session_name"])
	require.Equal(t, id, rows[0]["session_id"])
	require.Equal(t, "ready", rows[0]["cache_monitoring_status"])
	require.NotNil(t, rows[0]["cache_drop"])
	require.NotContains(t, rows[1], "session_name")
	require.NotContains(t, rows[1], "cache_drop")
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"`+id+`","thread_name":"Renamed"}`), 0600))
	require.Equal(t, "Renamed", decode()[0]["session_name"])
	repo.fail = true
	rows = decode()
	require.Equal(t, "unavailable", rows[0]["cache_monitoring_status"])
	require.NotContains(t, rows[0], "cache_drop")
	t.Setenv("PERSONAL_USAGE_LOCAL_TITLES", "false")
	require.NotContains(t, decode()[0], "session_name")
}

func TestAdminPersonalUsageRestrictedResponse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/usage", nil)
	c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{KeyID: "restricted"}))
	// A restricted principal must not invoke any local metadata or cost reader.
	h := &UsageHandler{}
	body, err := json.Marshal(h.upstreamPricingRows(c, []service.UsageLog{{ID: 1}}))
	require.NoError(t, err)
	for _, field := range []string{"session_name", "cache_drop", "cache_monitoring_status", "upstream_estimate"} {
		require.NotContains(t, string(body), field)
	}
}
