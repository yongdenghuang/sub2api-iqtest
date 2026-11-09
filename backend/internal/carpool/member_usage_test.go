//go:build unit

package carpool

import (
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOfficialWindowSnapshotsMatchDurationAndRejectInvalidReadings(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	five := &service.OpenAIRateLimitWindow{LimitWindowSeconds: 18000, UsedPercent: 12, ResetAt: now.Add(time.Hour).Unix()}
	seven := &service.OpenAIRateLimitWindow{LimitWindowSeconds: 604800, UsedPercent: 48, ResetAt: now.Add(6 * 24 * time.Hour).Unix()}
	usage := &service.OpenAIQuotaUsage{RateLimit: &service.OpenAIRateLimit{PrimaryWindow: seven, SecondaryWindow: five}}
	got := officialWindows(usage, now)
	require.Len(t, got, 2)
	require.Equal(t, "5h", got[0].Window)
	require.Equal(t, 12.0, got[0].UsedPercent)
	require.Equal(t, "7d", got[1].Window)
	require.Equal(t, 48.0, got[1].UsedPercent)
	for _, invalid := range []float64{math.NaN(), math.Inf(1), -1, 101} {
		five.UsedPercent = invalid
		require.Len(t, officialWindows(usage, now), 1)
	}
	five.UsedPercent = 0
	five.ResetAt = now.Add(-time.Minute).Unix()
	require.Len(t, officialWindows(usage, now), 2, "retain recently ended windows until an official update")
	for _, reset := range []int64{0, now.Add(-6 * time.Hour).Unix(), now.Add(6 * time.Hour).Unix()} {
		five.ResetAt = reset
		require.Len(t, officialWindows(usage, now), 1)
	}
	usage.RateLimit.SecondaryWindow = nil
	require.Len(t, officialWindows(usage, now), 1, "missing five-hour data must not use the weekly reading")
	require.Empty(t, officialWindows(nil, now))
}
