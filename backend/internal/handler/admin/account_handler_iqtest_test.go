package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestAccountIQFilterValidation(t *testing.T) {
	for _, value := range []string{"", "normal", "untested", "quality_fail", "infra_error", "invalid"} {
		t.Run(value, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/accounts?iq_quality="+value, nil)
			ok := applyAccountIQFilter(c)
			if value == "invalid" {
				require.False(t, ok)
				require.Equal(t, 400, w.Code)
			} else {
				require.True(t, ok)
				require.Equal(t, value, iqquality.FromContext(c.Request.Context()))
			}
		})
	}
	require.Equal(t, "quality_fail", toServiceBulkUpdateAccountFilters(&BulkUpdateAccountFilters{IQQuality: "quality_fail"}).IQQuality)
}
