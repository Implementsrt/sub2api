package admin

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStationTimingRestrictedAdminDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request = c.Request.WithContext(adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{KeyID: "restricted"}))
	(&UpstreamPricingHandler{}).Timing(c)
	require.Equal(t, 403, w.Code)
}
