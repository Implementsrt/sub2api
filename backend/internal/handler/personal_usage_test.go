package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type personalUsageRepo struct {
	service.UsageLogRepository
	rows          []service.UsageLog
	userID        int64
	estimateCalls int
}

func (r *personalUsageRepo) ListUpstreamEstimates(_ context.Context, userID int64, ids []int64) (map[int64]*upstreampricing.Estimate, error) {
	r.estimateCalls++
	if userID != 42 {
		return nil, nil
	}
	amount := "0.01"
	return map[int64]*upstreampricing.Estimate{11: {Status: "estimated", Amount: &amount}}, nil
}

func (r *personalUsageRepo) ListPersonalUsageContext(_ context.Context, userID int64, _ []service.UsageLog) ([]service.UsageLog, error) {
	r.userID = userID
	return r.rows, nil
}

func TestPersonalUsageRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	id := "01a1c88f-8e4e-78d1-852e-e9f46ffcd6c6"
	require.NoError(t, os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"id":"`+id+`","thread_name":"Local title"}`), 0600))
	saved := personalUsageTitles
	personalUsageTitles = &personalusage.TitleResolver{CodexHome: home, ClaudeHome: home}
	t.Cleanup(func() { personalUsageTitles = saved })
	t.Setenv("PERSONAL_USAGE_LOCAL_TITLES", "true")
	duration := 1000
	before := service.UsageLog{ID: 10, UserID: 42, SessionID: &id, AccountID: 1, Model: "gpt", InputTokens: 10, CacheReadTokens: 90, DurationMs: &duration, CreatedAt: time.Now().Add(-time.Minute)}
	after := before
	after.ID, after.CreatedAt, after.InputTokens, after.CacheReadTokens = 11, before.CreatedAt.Add(time.Minute), 40, 60
	repo := &personalUsageRepo{rows: []service.UsageLog{before, after}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/usage", nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), role)
		rows := h.personalUsageRows(c, []service.UsageLog{after}) // Predecessor is outside this page.
		require.Equal(t, int64(42), repo.userID)
		require.Len(t, rows, 1)
		require.Equal(t, "ready", rows[0].CacheMonitoringStatus)
		require.Equal(t, &id, rows[0].SessionID)
		require.NotNil(t, rows[0].CacheDrop)
		require.Equal(t, int64(10), rows[0].CacheDrop.PreviousRequestID)
		if role == service.RoleAdmin {
			require.Equal(t, "Local title", rows[0].SessionName)
			require.NotNil(t, rows[0].UpstreamEstimate)
			require.Equal(t, "0.01", *rows[0].UpstreamEstimate.Amount)
		} else {
			require.Empty(t, rows[0].SessionName)
			require.Nil(t, rows[0].UpstreamEstimate)
		}
		encoded, err := json.Marshal(rows[0])
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "upstream_model")
		if role != service.RoleAdmin {
			require.NotContains(t, string(encoded), "upstream_estimate")
		}
	}
	require.Equal(t, 1, repo.estimateCalls)
}

func TestPersonalUsageRowsUnavailable(t *testing.T) {
	id := "unknown-session"
	h := NewUsageHandler(service.NewUsageService(&userUsageRepoCapture{}, nil, nil, nil), nil, nil, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/usage", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	rows := h.personalUsageRows(c, []service.UsageLog{{ID: 1, UserID: 42, SessionID: &id}})
	require.Len(t, rows, 1)
	require.Equal(t, "unavailable", rows[0].CacheMonitoringStatus)
	require.Equal(t, &id, rows[0].SessionID)
	require.Empty(t, rows[0].SessionName)
	require.Nil(t, rows[0].CacheDrop)
}
