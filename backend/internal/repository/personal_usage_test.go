package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPersonalUsageContextIsolation(t *testing.T) {
	var captured string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(_, query string) error { captured = query; return nil })))
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	session := "session"
	mock.ExpectQuery("context").WithArgs(int64(42), int64(1)).WillReturnRows(sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")))
	_, err = repo.ListPersonalUsageContext(context.Background(), 42, []service.UsageLog{
		{ID: 1, UserID: 42, SessionID: &session},
		{ID: 2, UserID: 99, SessionID: &session}, // Caller cannot inject another user's records.
		{ID: 3, UserID: 42},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Contains(t, captured, "created_at = t.created_at")
	require.Contains(t, captured, "FETCH FIRST 1 ROW WITH TIES")
	require.Contains(t, captured, "INTERVAL '5 minutes'")
	require.Equal(t, 4, strings.Count(captured, "user_id = $1"))
	// A missing identity does not cause an unbounded database query.
	_, err = repo.ListPersonalUsageContext(context.Background(), 42, []service.UsageLog{{ID: 3, UserID: 42}})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
