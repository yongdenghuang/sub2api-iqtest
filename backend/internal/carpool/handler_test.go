//go:build unit

package carpool

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

func TestMemberViewRequiresAuthenticationAndHasNoMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, setSubject := range []bool{false, true} {
		r := gin.New()
		(&Service{}).RegisterMemberRoutes(r.Group("/api/v1"), func(c *gin.Context) {
			if setSubject {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 0})
			}
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/carpool?user_id=1", nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	r := gin.New()
	(&Service{}).RegisterMemberRoutes(r.Group("/api/v1"), func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/carpool", nil))
	require.Equal(t, http.StatusForbidden, w.Code, "member routes must apply the site's user guards")
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/carpool", nil))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
