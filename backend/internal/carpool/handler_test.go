//go:build unit

package carpool

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAllCarpoolRoutesRequireAdminMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s := &Service{}
	s.RegisterRoutes(r.Group("/api/v1"), func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	for _, req := range []struct{ method, path string }{{"GET", "/groups"}, {"PUT", "/groups/1"}, {"POST", "/groups/1/sync"}, {"GET", "/groups/1/events"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(req.method, "/api/v1/admin/carpool"+req.path, nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
}
