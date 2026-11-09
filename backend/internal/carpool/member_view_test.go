//go:build unit

package carpool

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemberSummaryRetainsOverspentAndStaleQuota(t *testing.T) {
	now := time.Now()
	st := State{CheckedAt: now, ResetAt: now.Add(time.Hour), Estimated: true, Capacity: 400, Percent: 50, Members: 2}
	for _, tc := range []struct {
		name   string
		change func(*State)
		status string
	}{
		{"fresh", func(*State) {}, "synced"},
		{"query failure", func(s *State) { s.LastError = "private upstream error" }, "delayed"},
		{"stale", func(s *State) { s.CheckedAt = now.Add(-4 * time.Minute) }, "delayed"},
		{"reset elapsed", func(s *State) { s.ResetAt = now.Add(-time.Minute) }, "pending_reset"},
		{"reset candidate", func(s *State) { s.Candidate = now.Add(week) }, "pending_reset"},
		{"membership changed", func(s *State) { s.Members = 3 }, "adjusting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := st
			tc.change(&state)
			g := MemberGroupView{PerMemberUSD: 100, SyncOfficialReset: true, Members: []MemberView{{Name: "private-other-member", UsedUSD: 150, RemainingUSD: 0}, {Name: "me", IsSelf: true, UsedUSD: 20, RemainingUSD: 80}}}
			completeMemberView(&g, state, now)
			require.Equal(t, 200.0, g.TotalUSD)
			require.Equal(t, 170.0, g.UsedUSD)
			require.Equal(t, 80.0, g.RemainingUSD, "overspending must not erase another member's available allowance")
			require.Equal(t, tc.status, g.Status)
			data, err := json.Marshal(g)
			require.NoError(t, err)
			require.NotContains(t, string(data), "private upstream error")
			require.NotContains(t, string(data), "private-other-member")
			require.Len(t, g.Members, 1)
			require.True(t, g.Members[0].IsSelf)
		})
	}
	g := MemberGroupView{}
	completeMemberView(&g, State{}, now)
	require.False(t, g.AllocationReady)
	require.Nil(t, g.OfficialUsedPercent, "unknown is not zero usage")
	require.Nil(t, g.NextResetAt)
	require.Nil(t, g.LastSyncAt)
	require.Nil(t, g.UpstreamCapacityUSD)
	require.Equal(t, "waiting", g.Status)
}

func TestManualMemberSummaryDoesNotClaimSamplingOrAutomaticAdjustments(t *testing.T) {
	now := time.Now()
	st := State{CheckedAt: now, ResetAt: now.Add(-time.Minute), Estimated: true, Capacity: 1000, Members: 5, DropAt: now}
	g := MemberGroupView{AllocationMode: AllocationManual, PerMemberUSD: 600, Members: []MemberView{{UsedUSD: 100, RemainingUSD: 500}}}
	completeMemberView(&g, st, now)
	require.Equal(t, "synced", g.Status)
	require.Equal(t, 600.0, g.TotalUSD)
	require.False(t, g.Estimated)
	require.Nil(t, g.UpstreamCapacityUSD)
	g = MemberGroupView{AllocationMode: AllocationManual, SyncOfficialReset: true, PerMemberUSD: 600}
	completeMemberView(&g, st, now)
	require.Equal(t, "pending_reset", g.Status)
}
