package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaRecoveryRepo struct {
	stubQuotaAccountRepo
	clears int
}

func (r *quotaRecoveryRepo) ClearOpenAIQuotaRateLimitIfObserved(_ context.Context, id int64, limited, reset time.Time) (bool, error) {
	r.clears++
	a := r.accounts[id]
	if a.RateLimitedAt == nil || a.RateLimitResetAt == nil || !a.RateLimitedAt.Equal(limited) || !a.RateLimitResetAt.Equal(reset) {
		return false, nil
	}
	clone := *a
	clone.RateLimitedAt, clone.RateLimitResetAt = nil, nil
	r.accounts[id] = &clone
	return true, nil
}

func availableQuota() *OpenAIQuotaUsage {
	return &OpenAIQuotaUsage{RateLimit: &OpenAIRateLimit{Allowed: true,
		PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 0, LimitWindowSeconds: 5 * 3600},
		SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 6, LimitWindowSeconds: 7 * 86400}}}
}

func TestQuotaRecoveryRequiresPositiveUpstreamEvidence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*OpenAIQuotaUsage)
		available bool
	}{
		{"reset card restored quota", func(*OpenAIQuotaUsage) {}, true},
		{"still blocked", func(u *OpenAIQuotaUsage) { u.RateLimit.Allowed = false }, false},
		{"limit reached", func(u *OpenAIQuotaUsage) { u.RateLimit.LimitReached = true }, false},
		{"weekly exhausted", func(u *OpenAIQuotaUsage) { u.RateLimit.SecondaryWindow.UsedPercent = 100 }, false},
		{"hourly exhausted", func(u *OpenAIQuotaUsage) { u.RateLimit.PrimaryWindow.UsedPercent = 100 }, false},
		{"missing envelope", func(u *OpenAIQuotaUsage) { u.RateLimit = nil }, false},
		{"missing windows", func(u *OpenAIQuotaUsage) { u.RateLimit.PrimaryWindow = nil; u.RateLimit.SecondaryWindow = nil }, false},
		{"empty window", func(u *OpenAIQuotaUsage) { u.RateLimit.PrimaryWindow = &OpenAIRateLimitWindow{} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := availableQuota()
			tc.change(u)
			require.Equal(t, tc.available, OpenAIQuotaAvailable(u))
		})
	}
	require.False(t, OpenAIQuotaAvailable(nil))
}

func TestRefreshUsageRecoversExternalResetWithoutConsumingCards(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			limited, reset := time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour)
			a := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false,
				RateLimitedAt: &limited, RateLimitResetAt: &reset, Credentials: map[string]any{"chatgpt_account_id": "test-workspace"}}
			repo := &quotaRecoveryRepo{stubQuotaAccountRepo: stubQuotaAccountRepo{accounts: map[int64]*Account{100: a}}}
			tokens := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(a): "test-token"}}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method, "recovery must not consume reset cards")
				require.NotContains(t, r.URL.Path, "consume")
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/backend-api/wham/usage" {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"limit_window_seconds":18000},"secondary_window":{"used_percent":6,"limit_window_seconds":604800}}}`))
				} else {
					_, _ = w.Write([]byte(`{"available_count":0,"credits":[]}`))
				}
			}))
			defer srv.Close()
			svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, tokens, nil), newQuotaRedirectingFactory(srv), nil)
			u, err := svc.RefreshUsage(context.Background(), a.ID)
			if status == http.StatusOK {
				require.NoError(t, err)
				require.True(t, u.RateLimitRecovered)
				require.Nil(t, repo.accounts[100].RateLimitResetAt)
				require.False(t, repo.accounts[100].Schedulable, "must preserve manual scheduling switch")
				require.Equal(t, 1, repo.clears)
			} else {
				require.Error(t, err)
				require.Zero(t, repo.clears)
				require.NotNil(t, repo.accounts[100].RateLimitResetAt)
			}
		})
	}
}

func TestQuotaRecoveryPreservesNew429AndAccountErrors(t *testing.T) {
	limited, reset := time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour)
	observed := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, RateLimitedAt: &limited, RateLimitResetAt: &reset}
	newLimited := time.Now()
	current := *observed
	current.RateLimitedAt = &newLimited
	repo := &quotaRecoveryRepo{stubQuotaAccountRepo: stubQuotaAccountRepo{accounts: map[int64]*Account{1: &current}}}
	svc := &OpenAIQuotaService{accountRepo: repo}
	u := availableQuota()
	require.NoError(t, svc.reconcileQuotaRecovery(context.Background(), observed, u))
	require.False(t, u.RateLimitRecovered)
	require.Equal(t, &newLimited, repo.accounts[1].RateLimitedAt)
	observed.Status = StatusError
	observed.ErrorMessage = "token_revoked"
	repo.clears = 0
	require.NoError(t, svc.reconcileQuotaRecovery(context.Background(), observed, availableQuota()))
	require.Zero(t, repo.clears)
	require.Equal(t, "token_revoked", observed.ErrorMessage)
}
