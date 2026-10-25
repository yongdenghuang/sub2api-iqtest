package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

// IQTestDeps exposes the two services the iqtest feature needs without
// changing the handler's fields or constructor.
func (h *AccountHandler) IQTestDeps() (service.AdminService, *service.AccountTestService) {
	return h.adminService, h.accountTestService
}
