//go:build unit

package carpool

import (
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOnlyFreshWeeklyQuotaIsAccepted(t *testing.T) {
	now := time.Now().UTC()
	w := &service.OpenAIRateLimitWindow{UsedPercent: 25, LimitWindowSeconds: 604800, ResetAt: now.Add(3 * 24 * time.Hour).Unix()}
	u := &service.OpenAIQuotaUsage{RateLimit: &service.OpenAIRateLimit{PrimaryWindow: &service.OpenAIRateLimitWindow{LimitWindowSeconds: 18000, UsedPercent: 100}, SecondaryWindow: w}}
	p, reset, err := weeklyObservation(u, now)
	require.NoError(t, err)
	require.Equal(t, 25.0, p)
	require.Equal(t, w.ResetAt, reset.Unix())
	for _, invalid := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		w.UsedPercent = invalid
		_, _, err = weeklyObservation(u, now)
		require.Error(t, err)
	}
	w.UsedPercent = 0
	w.ResetAt = now.Add(-time.Minute).Unix()
	_, _, err = weeklyObservation(u, now)
	require.Error(t, err)
	u.RateLimit.SecondaryWindow = nil
	_, _, err = weeklyObservation(u, now)
	require.Error(t, err)
	_, _, err = weeklyObservation(nil, now)
	require.Error(t, err)
}

func TestResetRequiresTwoConsistentObservations(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(2 * 24 * time.Hour)
	next := now.Add(week - time.Minute)
	st := State{ResetAt: old, Percent: 90, Estimated: true, Capacity: 2400}
	changed, ready := observeWindow(&st, 0, next, now)
	require.False(t, changed)
	require.False(t, ready)
	require.Equal(t, old, st.ResetAt)
	changed, ready = observeWindow(&st, 0, next, now.Add(30*time.Second))
	require.False(t, changed)
	require.False(t, ready)
	changed, ready = observeWindow(&st, 1, next, now.Add(time.Minute))
	require.True(t, changed)
	require.True(t, ready)
	require.Equal(t, next, st.ResetAt)
	require.False(t, st.Estimated)
	changed, ready = observeWindow(&st, 1, next, now.Add(2*time.Minute))
	require.False(t, changed)
	require.True(t, ready, "the same reset is never applied twice")
}

func TestPercentageDropResamplesWithoutReset(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(3 * 24 * time.Hour)
	st := State{ResetAt: reset, Percent: 70, Capacity: 2000, Estimated: true, Baseline: Observation{At: now.Add(-time.Hour), Percent: 60, Cost: 100}}
	changed, ready := observeWindow(&st, 30, reset, now)
	require.False(t, changed)
	require.False(t, ready)
	changed, ready = observeWindow(&st, 31, reset, now.Add(time.Minute))
	require.False(t, changed)
	require.True(t, ready)
	require.Equal(t, reset, st.ResetAt)
	require.True(t, st.Baseline.At.IsZero())
	require.Equal(t, 2000.0, st.Capacity)
}

func TestNaturalResetAndTimestampDrift(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	next := old.Add(week)
	st := State{ResetAt: old, Percent: 0}
	_, ready := observeWindow(&st, 0, next, now)
	require.False(t, ready)
	changed, ready := observeWindow(&st, 0, next, now.Add(time.Minute))
	require.True(t, changed)
	require.True(t, ready)
	changed, ready = observeWindow(&st, 1, next.Add(time.Minute), now.Add(2*time.Minute))
	require.False(t, changed)
	require.True(t, ready)
	require.Equal(t, next, st.ResetAt)
	st = State{ResetAt: next, Percent: 60}
	_, ready = observeWindow(&st, 60, next.Add(time.Hour), now)
	require.False(t, ready, "time advance alone before reset is insufficient")
	_, ready = observeWindow(&st, 60, next.Add(-time.Hour), now)
	require.False(t, ready, "old cached window cannot rewind counters")
}

func TestCapacityUsesFreshCostDeltas(t *testing.T) {
	now := time.Now().UTC()
	st := State{Capacity: 2400}
	estimate(&st, Observation{At: now, Percent: 20, Cost: 9000})
	require.False(t, st.Estimated, "old retained costs are not a baseline")
	estimate(&st, Observation{At: now.Add(3 * time.Minute), Percent: 24, Cost: 9100})
	require.False(t, st.Estimated, "need at least 5 percentage points")
	estimate(&st, Observation{At: now.Add(4 * time.Minute), Percent: 29, Cost: 9200})
	require.True(t, st.Estimated)
	require.InDelta(t, 2000, st.Capacity, 0.001)
	estimate(&st, Observation{At: now.Add(10 * time.Minute), Percent: 39, Cost: 200})
	require.False(t, st.Estimated, "log retention/counter changes must restart sampling")
}

func TestEqualCapsCannotRecycleSpentQuota(t *testing.T) {
	require.Equal(t, 600.0, fairLimit(State{Capacity: 2400}, []float64{50, 170, 0, 160}, 0))
	require.Equal(t, 1100.0, fairLimit(State{Capacity: 2400}, []float64{100, 0}, 200), "departed member's cost remains spent")
	require.Equal(t, 100.0, fairLimit(State{Estimated: true, Capacity: 1000, Percent: 90}, []float64{1000, 0}, 0), "averaging would incorrectly grant 550 when only 100 remains")
	require.Equal(t, 50.0, fairLimit(State{Estimated: true, Capacity: 1000, Percent: 90}, []float64{1000, 0, 0}, 0))
	require.Equal(t, 0.0, fairLimit(State{Capacity: 1000}, nil, 0))
	for n := 1; n <= 10; n++ {
		usage := make([]float64, n)
		for i := range usage {
			usage[i] = float64(i * 137)
		}
		st := State{Estimated: true, Capacity: 1700, Percent: 43}
		cap := fairLimit(st, usage, 0)
		var available float64
		for _, used := range usage {
			available += math.Max(0, cap-used)
		}
		require.LessOrEqual(t, available, st.Capacity*(1-st.Percent/100)+0.001)
	}
	require.Greater(t, fairLimit(State{Estimated: true, Capacity: 1000, Percent: 100}, []float64{0}, 0), 0.0, "zero means unlimited in native groups")
}
