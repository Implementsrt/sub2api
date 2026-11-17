package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/upstreampricing"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLocalBrowserImportRejectsRemoteAndForwardedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, input := range []struct{ peer, forwarded string }{{"192.0.2.1:1234", ""}, {"127.0.0.1:1234", "192.0.2.1"}} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		m := upstreampricing.NewManager(&upstreampricing.Store{DB: db}, nil, nil)
		h := NewUpstreamPricingHandler(&service.UpstreamPricingService{Manager: m})
		mock.ExpectQuery("SELECT type, platform, credentials FROM accounts").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"type", "platform", "credentials"}).AddRow("apikey", "openai", []byte(`{"api_key":"synthetic"}`)))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/admin/upstream-pricing/accounts/7/local-browser-import", nil)
		c.Request.RemoteAddr = input.peer
		c.Request.Header.Set("Origin", "http://localhost:8080")
		if input.forwarded != "" {
			c.Request.Header.Set("X-Forwarded-For", input.forwarded)
		}
		c.Params = gin.Params{{Key: "id", Value: "7"}}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		h.LocalBrowserImport(c)
		require.Equal(t, 403, w.Code)
		require.NoError(t, mock.ExpectationsWereMet())
		m.Stop()
		db.Close()
	}
}
