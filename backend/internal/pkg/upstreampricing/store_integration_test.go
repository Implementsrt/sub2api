package upstreampricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Opt-in: all fixtures live in a unique disposable schema; the service's schema
// and credentials are never read by this test or printed in failure messages.
func TestUpstreamPricingStoreIntegration(t *testing.T) {
	dsn := os.Getenv("FORK_PRICING_TEST_DSN")
	if dsn == "" {
		t.Skip("set FORK_PRICING_TEST_DSN to run isolated PostgreSQL integration")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("cannot open integration database")
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if root.PingContext(ctx) != nil {
		t.Fatal("integration database connection failed")
	}
	schema := "test_fork_pricing_" + randomToken()
	_, err = root.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	defer func() {
		_, err := root.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		require.NoError(t, err)
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("integration DSN must be a PostgreSQL URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("cannot open isolated integration schema")
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts(id bigint PRIMARY KEY,type text,platform text,credentials jsonb,deleted_at timestamptz);CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,account_id bigint,api_key_id bigint,request_id text)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../../migrations/252_fork_upstream_pricing.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO accounts VALUES(7000000001,'apikey','openai','{"base_url":"https://example.com","api_key":"synthetic-inference-key"}',NULL)`)
	require.NoError(t, err)
	s := &Store{DB: db}
	id := int64(7000000001)
	fp, err := s.AccountFingerprint(ctx, id)
	require.NoError(t, err)
	src, err := s.Source(ctx, id)
	require.NoError(t, err)
	require.Zero(t, src.Revision)
	require.True(t, src.Config.Automatic, "new sources default to automatic sync")
	c := Config{Provider: "newapi", BaseURL: "https://example.com", Group: "paid", IntervalMinutes: 60}
	cat := fixtureCatalog()
	require.NoError(t, finalize(&cat, nil))
	preview, err := s.SavePreview(ctx, id, 0, c, "synthetic-encrypted-value", fp, cat)
	require.NoError(t, err)
	version1, err := s.ApplyPreview(ctx, id, preview.ID)
	require.NoError(t, err)
	require.Positive(t, version1)
	src, err = s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(1), src.Revision)
	require.True(t, src.HasCredential)
	public, _ := json.Marshal(src)
	require.NotContains(t, string(public), "synthetic-encrypted-value")
	require.Empty(t, src.Config.Token)
	require.False(t, src.Config.Automatic, "an explicitly disabled source remains disabled")
	_, err = db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN name text NOT NULL DEFAULT 'Fixture account'`)
	require.NoError(t, err)
	comparison, err := s.Comparison(ctx, "mapped", 0, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 1, comparison.Total)
	require.Len(t, comparison.Items, 1)
	require.Equal(t, "mapped", comparison.Items[0].Model)
	require.Equal(t, version1, comparison.Items[0].VersionID)
	require.False(t, comparison.Accounts[0].AccountChanged)
	publicComparison, _ := json.Marshal(comparison)
	require.NotContains(t, string(publicComparison), "synthetic-")
	empty, err := s.Comparison(ctx, "mapped", id+1, 1, 100)
	require.NoError(t, err)
	require.Zero(t, empty.Total)
	versions, err := s.Versions(ctx, id)
	require.NoError(t, err)
	oldStart := versions[0].EffectiveFrom
	changedRatio := cat
	changedRatio.Group.Multiplier = "0.25"
	require.NoError(t, finalize(&changedRatio, nil))
	ratioPreview, err := s.SavePreview(ctx, id, src.Revision, c, "synthetic-encrypted-value", fp, changedRatio)
	require.NoError(t, err)
	require.Equal(t, 1, ratioPreview.Changed)
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs VALUES(100,42,7000000001,9,'request-1'),(101,42,7000000001,9,'request-2'),(102,99,7000000001,9,'request-3')`)
	require.NoError(t, err)
	usage := Usage{AccountID: id, AccountFingerprint: fp, PricingAt: oldStart, Model: "mapped", Input: 1000000}
	// A price change happens before the original request's usage job is saved.
	src, lease, err := s.Claim(ctx, id)
	require.NoError(t, err)
	cat.Models[0].Input = ptr("4")
	require.NoError(t, finalize(&cat, nil))
	version2, err := s.PublishSync(ctx, src, lease, cat, "manual_sync")
	require.NoError(t, err)
	require.NotEqual(t, version1, version2)
	job := UsageJob{ID: 100, UserID: 42, APIKeyID: 9, RequestID: "request-1", Usage: usage}
	require.NoError(t, s.Enqueue(ctx, job))
	require.NoError(t, s.Enqueue(ctx, job))
	require.NoError(t, s.Process(ctx))
	values, err := s.Estimates(ctx, 42, []int64{100, 102})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, version1, values[100].VersionID)
	require.Equal(t, "1", *values[100].Amount)
	// Old history is not recalculated using today's prices.
	job.ID = 101
	job.RequestID = "request-2"
	job.Usage.PricingAt = oldStart.Add(-time.Second)
	require.NoError(t, s.Enqueue(ctx, job))
	require.NoError(t, s.Process(ctx))
	values, err = s.Estimates(ctx, 42, []int64{101})
	require.NoError(t, err)
	require.Equal(t, "not_configured", values[101].Status)
	require.Nil(t, values[101].Amount)
	// Same catalog records a successful sync without making another price version.
	src, lease, err = s.Claim(ctx, id)
	require.NoError(t, err)
	same, err := s.PublishSync(ctx, src, lease, cat, "scheduled")
	require.NoError(t, err)
	require.Equal(t, version2, same)
	// Persisted lease prevents independent workers from claiming the same account.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	tokens := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, token, err := s.Claim(ctx, id)
			results <- err
			if err == nil {
				tokens <- token
			}
		}()
	}
	wg.Wait()
	close(results)
	won, busy := 0, 0
	for err := range results {
		if err == nil {
			won++
		} else if err == ErrBusy {
			busy++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, won)
	require.Equal(t, 1, busy)
	current, err := s.Source(ctx, id)
	require.NoError(t, err)
	require.NoError(t, s.Fail(ctx, current, <-tokens, "scheduled", "synthetic safe failure"))
	after, err := s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, version2, *after.ActiveVersion)
	require.Equal(t, "paid", after.Config.Group)
	// Editing a schedule invalidates a preview and an in-flight sync.
	preview, err = s.SavePreview(ctx, id, after.Revision, c, "synthetic-encrypted-value", fp, cat)
	require.NoError(t, err)
	claimed, token, err := s.Claim(ctx, id)
	require.NoError(t, err)
	require.NoError(t, s.Schedule(ctx, id, true, 60))
	_, err = s.ApplyPreview(ctx, id, preview.ID)
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.PublishSync(ctx, claimed, token, cat, "scheduled")
	require.ErrorIs(t, err, ErrConflict)
	// A changed model key cannot reuse a pending preview from the old identity.
	current, err = s.Source(ctx, id)
	require.NoError(t, err)
	preview, err = s.SavePreview(ctx, id, current.Revision, c, "synthetic-encrypted-value", fp, cat)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET credentials='{"base_url":"https://example.com","api_key":"changed-synthetic-key"}' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = s.ApplyPreview(ctx, id, preview.ID)
	require.ErrorIs(t, err, ErrConflict)
	// All test state remains in the test schema, and stored configs contain no token.
	comparison, err = s.Comparison(ctx, "mapped", id, 1, 100)
	require.NoError(t, err)
	require.True(t, comparison.Accounts[0].AccountChanged)
	var configs string
	err = db.QueryRowContext(ctx, `SELECT string_agg(config::text,'') FROM fork_upstream_pricing_sources`).Scan(&configs)
	require.NoError(t, err)
	require.False(t, strings.Contains(configs, "synthetic-encrypted-value"))

	t.Run("browser imports verify and reuse credentials without saving configuration", func(t *testing.T) {
		invalidate := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/user/self/groups", r.URL.Path)
			require.Equal(t, "Bearer synthetic-panel", r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			if invalidate {
				require.NoError(t, s.Schedule(ctx, id, true, 60))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"paid":{"ratio":0.5}}}`))
		}))
		defer server.Close()
		m := NewManager(s, NewRemote(""), timingTestCipher{})
		defer m.Stop()
		before, e := s.Source(ctx, id)
		require.NoError(t, e)
		candidate := Config{Provider: "newapi", BaseURL: server.URL, Group: "paid", Automatic: true, IntervalMinutes: 60}
		start, e := m.StartBrowserImport(ctx, 1, id, before.Revision, "http://localhost:8080", candidate)
		require.NoError(t, e)
		require.NoError(t, m.CompleteBrowserImport(ctx, start.Ticket, "http://localhost:8080", "synthetic-panel", "42"))
		require.Error(t, m.CompleteBrowserImport(ctx, start.Ticket, "http://localhost:8080", "synthetic-panel", "42"))
		status, e := m.Imports.Status(start.ID, 1, id)
		require.NoError(t, e)
		require.Equal(t, "ready", status.Status)
		require.Equal(t, "0.5", status.Groups[0].Multiplier)
		candidate.BrowserImportID, candidate.UserID = start.ID, "42"
		groups, e := m.Groups(ctx, id, candidate)
		require.NoError(t, e)
		require.Equal(t, "paid", groups[0].Name)
		after, e := s.Source(ctx, id)
		require.NoError(t, e)
		require.Equal(t, before.Revision, after.Revision)
		require.Equal(t, before.Config, after.Config)
		invalidate = true
		start, e = m.StartBrowserImport(ctx, 1, id, after.Revision, "http://localhost:8080", candidate)
		require.NoError(t, e)
		require.Error(t, m.CompleteBrowserImport(ctx, start.Ticket, "http://localhost:8080", "synthetic-panel", "42"))
		status, e = m.Imports.Status(start.ID, 1, id)
		require.NoError(t, e)
		require.Equal(t, "failed", status.Status)
	})
}
