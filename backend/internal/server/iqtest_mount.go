package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/iqtest"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// mountIQTest starts the scheduled quality-check feature and mounts its admin
// API. A failure is logged and never prevents the server from starting.
func mountIQTest(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	adminAuth middleware2.AdminAuthMiddleware,
	auditLog middleware2.AuditLogMiddleware,
	cfg *config.Config,
	rdb *redis.Client,
) {
	if h == nil || h.Admin == nil || h.Admin.Account == nil {
		return
	}
	admin, tester := h.Admin.Account.IQTestDeps()
	if admin == nil || tester == nil {
		return
	}
	svc, err := iqtest.Setup(iqtest.Deps{Cfg: cfg, Redis: rdb, Accounts: admin, Tester: tester})
	if err != nil {
		slog.Error("iqtest disabled: setup failed", "error", err)
		return
	}
	svc.RegisterRoutes(v1, gin.HandlerFunc(adminAuth), gin.HandlerFunc(auditLog))
}
