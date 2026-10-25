package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// IQTestDeps exposes the two services the iqtest feature needs without
// changing the handler's fields or constructor.
func (h *AccountHandler) IQTestDeps() (service.AdminService, *service.AccountTestService) {
	return h.adminService, h.accountTestService
}

func applyAccountIQFilter(c *gin.Context) bool {
	ctx, err := iqquality.WithFilter(c.Request.Context(), c.Query("iq_quality"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return false
	}
	c.Request = c.Request.WithContext(ctx)
	return true
}
