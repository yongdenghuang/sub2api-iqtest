//go:build unit

package carpool

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigCompatibilityAndIndependentReset(t *testing.T) {
	cfg, err := normalizeConfig(Config{Enabled: true, InitialTotalUSD: 2400}, Group{})
	require.NoError(t, err)
	require.Equal(t, AllocationAuto, cfg.AllocationMode)
	require.True(t, cfg.syncOfficialReset())
	off := false
	current := Group{WeeklyLimit: 600, Config: Config{AllocationMode: AllocationManual, SyncOfficialReset: &off}}
	cfg, err = normalizeConfig(Config{Enabled: true}, current)
	require.NoError(t, err)
	require.Equal(t, AllocationManual, cfg.AllocationMode)
	require.Equal(t, 600.0, cfg.ManualWeeklyUSD)
	require.False(t, cfg.syncOfficialReset(), "legacy saves preserve independently configured reset policy")
	cfg, err = normalizeConfig(Config{Enabled: true, AllocationMode: AllocationAuto, InitialTotalUSD: 2400}, current)
	require.NoError(t, err)
	require.False(t, cfg.syncOfficialReset(), "changing allocation mode must not re-enable upstream resets")
}

func TestConfigRejectsInvalidBudgets(t *testing.T) {
	for _, value := range []float64{0, -1, 0.99, 1000001, math.NaN(), math.Inf(1)} {
		_, err := normalizeConfig(Config{AllocationMode: AllocationManual, ManualWeeklyUSD: value}, Group{})
		require.Error(t, err)
		_, err = normalizeConfig(Config{AllocationMode: AllocationAuto, InitialTotalUSD: value}, Group{})
		require.Error(t, err)
	}
	_, err := normalizeConfig(Config{AllocationMode: "unknown", InitialTotalUSD: 2400}, Group{})
	require.Error(t, err)
}
