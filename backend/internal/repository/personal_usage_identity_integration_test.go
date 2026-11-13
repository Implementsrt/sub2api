package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPersonalUsageIdentityIntegration(t *testing.T) {
	dsn := os.Getenv("FORK_PRICING_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL integration DSN")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("cannot open integration database")
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if root.PingContext(ctx) != nil {
		t.Fatal("integration database unavailable")
	}
	schema := "test_fork_identity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = root.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	defer func() {
		_, err := root.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		require.NoError(t, err)
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid integration URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("cannot open isolated database")
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE usage_logs(id bigint PRIMARY KEY, user_id bigint, api_key_id bigint, request_id text, session_id text, created_at timestamptz)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/253_fork_usage_execution_identity.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	at := time.Now().UTC().Truncate(time.Second)
	// Parent, siblings and guardian interleave with two real child predecessors.
	fixtures := []struct {
		id, user, key    int64
		thread, subagent string
		sec              int
	}{
		{1, 42, 8, "child-a", "", 0}, {2, 42, 8, "parent", "", 10},
		{3, 42, 8, "child-b", "", 20}, {4, 99, 8, "child-a", "", 30},
		{5, 42, 9, "child-a", "", 40}, {6, 42, 8, "parent", "guardian", 50},
		{7, 42, 8, "child-a", "", 60}, {8, 42, 8, "child-a", "", 60},
		{9, 42, 8, "child-a", "", 90},
	}
	for _, f := range fixtures {
		rid := uuid.NewString()
		_, err = db.ExecContext(ctx, `INSERT INTO usage_logs VALUES($1,$2,$3,$4,'shared-parent',$5)`, f.id, f.user, f.key, rid, at.Add(time.Duration(f.sec)*time.Second))
		require.NoError(t, err)
		row := &service.UsageLog{RequestID: rid, APIKeyID: f.key, UserID: f.user, ForkIdentity: &personalusage.Identity{ThreadID: f.thread, Subagent: f.subagent, Source: "client_metadata_v1"}}
		require.NoError(t, repo.RecordPersonalUsageIdentity(ctx, row))
		// Retrying a request never replaces its original immutable identity.
		row.ForkIdentity.ThreadID = "wrong-overwrite"
		require.NoError(t, repo.RecordPersonalUsageIdentity(ctx, row))
	}
	queryIDs := func(target int64) []int64 {
		rows, err := db.QueryContext(ctx, personalUsageContextQuery([]string{"$2"}, "id"), int64(42), target)
		require.NoError(t, err)
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		require.NoError(t, rows.Err())
		return ids
	}
	require.ElementsMatch(t, []int64{1, 7, 8}, queryIDs(7), "cross-page predecessor ignores parent, sibling, guardian, other user and other key")
	require.ElementsMatch(t, []int64{7, 8, 9}, queryIDs(9), "equal-time predecessor ties remain ambiguous")
	identities, err := repo.ListPersonalUsageIdentities(ctx, 42, []service.UsageLog{{ID: 1, UserID: 42}, {ID: 4, UserID: 99}})
	require.NoError(t, err)
	require.Len(t, identities, 1)
	require.Equal(t, "child-a", identities[1].ThreadID)
	var session string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT session_id FROM usage_logs WHERE id=1`).Scan(&session))
	require.Equal(t, "shared-parent", session)
	_, err = db.ExecContext(ctx, `DELETE FROM usage_logs WHERE id=1`)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM fork_usage_execution_identity WHERE usage_id=1`).Scan(&remaining))
	require.Zero(t, remaining)
}
