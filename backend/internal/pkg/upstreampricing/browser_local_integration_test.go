package upstreampricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalBrowserImportIntegration(t *testing.T) {
	dsn := os.Getenv("FORK_PRICING_TEST_DSN")
	if dsn == "" {
		t.Skip("opt-in isolated PostgreSQL integration")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("cannot open integration database")
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := "test_fork_browser_" + randomToken()
	_, err = root.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	defer func() { _, _ = root.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) }()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("test DSN must be a PostgreSQL URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("cannot open isolated schema")
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts(id bigint PRIMARY KEY,type text,platform text,credentials jsonb,deleted_at timestamptz); CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,account_id bigint);`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../../migrations/252_fork_upstream_pricing.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO accounts VALUES(1,'apikey','openai','{"api_key":"synthetic-inference"}',NULL)`)
	require.NoError(t, err)
	good := localBrowserTestJWT("42", time.Now().Add(time.Hour))
	other := localBrowserTestJWT("99", time.Now().Add(time.Hour))
	var remoteUser, meCalls atomic.Int64
	remoteUser.Store(42)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+good {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/v1/auth/me":
			meCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": remoteUser.Load()}})
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"data":[{"id":5,"name":" paid ","rate_multiplier":1}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"data":{"5":0.5}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer site.Close()
	store := &Store{DB: db}
	m := NewManager(store, NewRemote(""), timingTestCipher{})
	defer m.Stop()
	c := Config{Provider: "sub2api", BaseURL: site.URL, IntervalMinutes: 60}
	reader := func(context.Context, string) ([]string, error) { return []string{good}, nil }
	value, err := m.importLocalFirefox(ctx, 1, 1, 0, "http://localhost:8080", c, reader)
	require.NoError(t, err)
	require.Equal(t, "ready", value.Status)
	require.Equal(t, " paid ", value.Groups[0].Name)
	public, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(public), good)
	require.NotContains(t, string(public), "signature")
	source, err := store.Source(ctx, 1)
	require.NoError(t, err)
	require.False(t, source.HasCredential, "read only stages an import, preview/apply still owns persistence")
	before := meCalls.Load()
	_, err = m.importLocalFirefox(ctx, 1, 1, 0, "http://localhost:8080", c, func(context.Context, string) ([]string, error) { return []string{good, other}, nil })
	require.Error(t, err)
	require.Equal(t, before, meCalls.Load(), "ambiguous users must not be auto-selected")
	fp, err := store.AccountFingerprint(ctx, 1)
	require.NoError(t, err)
	old := localBrowserTestJWT("42", time.Now().Add(-time.Hour))
	_, err = db.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_sources(account_id,revision,config,credential,account_fingerprint) VALUES(1,1,$1::jsonb,$2,$3)`, string(encoded(c)), "fixture:"+old, fp)
	require.NoError(t, err)
	_, err = m.importLocalFirefox(ctx, 1, 1, 1, "http://localhost:8080", c, func(context.Context, string) ([]string, error) { return []string{other, good}, nil })
	require.NoError(t, err, "expired saved JWT still pins the expected user")
	_, err = m.importLocalFirefox(ctx, 1, 1, 1, "http://localhost:8080", c, func(context.Context, string) ([]string, error) { return []string{other}, nil })
	require.Error(t, err)
	remoteUser.Store(99)
	_, err = m.importLocalFirefox(ctx, 1, 1, 1, "http://localhost:8080", c, reader)
	require.Error(t, err, "station must authenticate the same claimed identity")
	remoteUser.Store(42)
	_, err = m.importLocalFirefox(ctx, 1, 1, 1, "http://localhost:8080", c, func(context.Context, string) ([]string, error) {
		_, e := db.ExecContext(ctx, `UPDATE accounts SET credentials='{"api_key":"synthetic-changed"}' WHERE id=1`)
		require.NoError(t, e)
		return []string{good}, nil
	})
	require.ErrorIs(t, err, ErrConflict)
	source, err = store.Source(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "fixture:"+old, source.Credential, "reads must never silently replace the saved credential")
}
