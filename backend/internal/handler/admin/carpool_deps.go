package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// CarpoolQuota deliberately exposes only reads, never reset-card redemption.
func (h *OpenAIOAuthHandler) CarpoolQuota() interface {
	QueryUsage(context.Context, int64) (*service.OpenAIQuotaUsage, error)
} {
	return h.quotaService
}

func (h *SubscriptionHandler) SetCarpoolWeeklyController(manages func(int64) bool) {
	h.subscriptionService.SetCarpoolWeeklyController(manages)
}

func (h *SubscriptionHandler) InvalidateCarpoolSubscriptions(ctx context.Context, groupID int64, userIDs []int64) error {
	return h.subscriptionService.InvalidateCarpoolSubscriptions(ctx, groupID, userIDs)
}
