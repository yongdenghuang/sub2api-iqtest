//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCarpoolDoesNotResetOnLocalClockAlone(t *testing.T) {
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	sub := &UserSubscription{ID: 1, UserID: 1, GroupID: 12, Status: SubscriptionStatusActive, StartsAt: old.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), WeeklyWindowStart: &old, WeeklyUsageUSD: 650}
	stub := &resetQuotaUserSubRepoStub{sub: sub}
	svc := newResetQuotaSvc(stub)
	svc.now = func() time.Time { return now }
	svc.SetCarpoolWeeklyController(func(id int64) bool { return id == 12 })
	limit := 600.0
	g := &Group{WeeklyLimitUSD: &limit}
	_, err := svc.ValidateAndCheckLimits(sub, g)
	require.ErrorIs(t, err, ErrWeeklyLimitExceeded)
	require.Equal(t, 650.0, sub.WeeklyUsageUSD)
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.False(t, stub.resetWeeklyCalled)
	displayed := []UserSubscription{*sub}
	normalizeExpiredWindowsAt(displayed, now, svc.carpoolManagesWeekly)
	require.Equal(t, 650.0, displayed[0].WeeklyUsageUSD)
	svc.SetCarpoolWeeklyController(nil)
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.True(t, stub.resetWeeklyCalled, "disabling restores native rolling windows")
}
