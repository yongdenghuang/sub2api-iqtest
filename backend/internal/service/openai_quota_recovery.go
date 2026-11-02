package service

import (
	"context"
	"math"
	"time"
)

// OpenAIQuotaAvailable requires an affirmative, complete upstream observation.
// A missing window, an auth/network failure or a local cached 0% is not proof
// that a reset card has restored the account's quota.
func OpenAIQuotaAvailable(usage *OpenAIQuotaUsage) bool {
	if usage == nil || usage.RateLimit == nil {
		return false
	}
	r := usage.RateLimit
	if !r.Allowed || r.LimitReached || (r.PrimaryWindow == nil && r.SecondaryWindow == nil) {
		return false
	}
	for _, w := range []*OpenAIRateLimitWindow{r.PrimaryWindow, r.SecondaryWindow} {
		if w != nil && (w.LimitWindowSeconds <= 0 || math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent >= 100) {
			return false
		}
	}
	return true
}

// RefreshUsage is the stateful counterpart of QueryUsage, for the admin POST
// and background recovery monitor. It never consumes reset credits. Capture
// the cooldown BEFORE querying so an in-flight success cannot clear a new 429.
func (s *OpenAIQuotaService) RefreshUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Also rotate failed probes to the back of the monitor's queue.
	if err := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		"iqtest_quota_checked_at": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return nil, err
	}
	usage, err := s.QueryUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.reconcileQuotaRecovery(ctx, account, usage); err != nil {
		return nil, err
	}
	return usage, nil
}

func (s *OpenAIQuotaService) reconcileQuotaRecovery(ctx context.Context, account *Account, usage *OpenAIQuotaUsage) error {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() {
		return nil
	}
	if updates := buildOpenAIAutoResetUsageUpdates(usage, time.Now()); len(updates) > 0 {
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
			return err
		}
	}
	if !OpenAIQuotaAvailable(usage) || account.Status != StatusActive || account.RateLimitedAt == nil || account.RateLimitResetAt == nil {
		return nil
	}
	repo, ok := s.accountRepo.(interface {
		ClearOpenAIQuotaRateLimitIfObserved(context.Context, int64, time.Time, time.Time) (bool, error)
	})
	if !ok {
		return nil // Never fall back to an unconditional cooldown clear.
	}
	cleared, err := repo.ClearOpenAIQuotaRateLimitIfObserved(ctx, account.ID, *account.RateLimitedAt, *account.RateLimitResetAt)
	if err == nil {
		usage.RateLimitRecovered = cleared
	}
	// This only clears the observed quota cooldown: status, scheduling switch,
	// auth failures, overload and unrelated model limits remain untouched.
	return err
}
