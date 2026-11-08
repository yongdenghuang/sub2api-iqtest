package server

import (
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/carpool"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func mountCarpool(v1 *gin.RouterGroup, h *handler.Handlers, jwtAuth middleware2.JWTAuthMiddleware, adminAuth middleware2.AdminAuthMiddleware, auditLog middleware2.AuditLogMiddleware, cfg *config.Config, settingService *service.SettingService, panelRateLimiter *middleware2.PanelRateLimiter) {
	if h == nil || h.Admin == nil || h.Admin.Account == nil || h.Admin.OpenAIOAuth == nil || h.Admin.Subscription == nil {
		return
	}
	admin, _ := h.Admin.Account.IQTestDeps()
	invalidator, ok := admin.(carpool.GroupInvalidator)
	if !ok {
		return
	}
	svc, err := carpool.Setup(cfg, h.Admin.OpenAIOAuth.CarpoolQuota(), invalidator, h.Admin.Subscription)
	if err != nil {
		slog.Error("carpool disabled: setup failed", "error", err)
		return
	}
	h.Admin.Subscription.SetCarpoolWeeklyController(svc.ManagesWeeklyWindow)
	svc.RegisterRoutes(v1, gin.HandlerFunc(adminAuth), gin.HandlerFunc(auditLog))
	svc.RegisterMemberRoutes(v1, gin.HandlerFunc(jwtAuth), middleware2.BackendModeUserGuard(settingService), panelRateLimiter.Global())
}
