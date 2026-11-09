//go:build integration

package carpool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testPersonalWindowUsage(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	var original []byte
	require.NoError(t, s.db.QueryRow("SELECT state FROM carpool_groups WHERE group_id=1").Scan(&original))
	t.Cleanup(func() {
		_, _ = s.db.Exec("UPDATE carpool_groups SET state=$1 WHERE group_id=1", original)
		_, _ = s.db.Exec("DELETE FROM usage_logs WHERE id>=9001")
	})
	var st State
	require.NoError(t, json.Unmarshal(original, &st))
	st.Windows = []OfficialWindow{{Window: "5h", UsedPercent: 12, ResetAt: now.Add(time.Hour)}, {Window: "7d", UsedPercent: 25, ResetAt: now.Add(6 * 24 * time.Hour)}}
	put := func() {
		raw, err := json.Marshal(st)
		require.NoError(t, err)
		_, err = s.db.Exec("UPDATE carpool_groups SET state=$1 WHERE group_id=1", raw)
		require.NoError(t, err)
	}
	put()
	_, err := s.db.Exec(`INSERT INTO usage_logs(id,user_id,group_id,account_id,subscription_id,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,account_stats_cost,account_rate_multiplier,actual_cost,created_at) VALUES
 (9001,1,1,1,1,10,20,30,40,5,NULL,2,3,$1::timestamptz-interval '1 hour'),
 (9002,1,1,1,1,20,0,0,0,8,4,3,2,$1::timestamptz-interval '8 hours'),
 (9003,2,1,1,2,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour'),
 (9004,1,2,1,10,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour'),
 (9005,1,1,2,1,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour'),
 (9006,1,1,1,2,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour'),
 (9007,1,1,1,1,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 day 1 second'),
 (9008,1,1,1,1,999999,0,0,0,999,NULL,1,999,$1::timestamptz+interval '2 hours'),
 (9009,1,1,1,NULL,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour'),
 (9010,1,1,1,1,7,0,0,0,1,NULL,NULL,0.5,$1::timestamptz-interval '4 hours'),
 (9011,1,1,1,1,11,0,0,0,2,NULL,NULL,1,$1::timestamptz-interval '1 day'),
 (9012,1,NULL,1,1,999999,0,0,0,999,NULL,1,999,$1::timestamptz-interval '1 hour')`, now)
	require.NoError(t, err)
	mine, err := s.ListForMember(ctx, 1)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	windows := mine[0].Windows
	require.Len(t, windows, 2)
	require.Equal(t, &MemberWindowStats{Requests: 2, Tokens: 107, AccountCostUSD: 11, UserCostUSD: 3.5}, windows[0].Stats)
	require.Equal(t, &MemberWindowStats{Requests: 4, Tokens: 138, AccountCostUSD: 25, UserCostUSD: 6.5}, windows[1].Stats)
	require.Equal(t, "official", windows[0].RangeSource)
	require.Equal(t, 12.0, *windows[0].OfficialUsedPercent)
	require.Equal(t, 25.0, *windows[1].OfficialUsedPercent)
	require.Len(t, mine[0].Members, 1)
	require.Equal(t, 100.0, mine[0].Members[0].UsedUSD, "window reporting must not overwrite subscription billing counters")
	other, err := s.ListForMember(ctx, 3)
	require.NoError(t, err)
	require.Equal(t, &MemberWindowStats{}, other[0].Windows[0].Stats, "a member with no logs sees their own zero, not car-wide traffic")
	// An ended official window falls back to recent local traffic, clearly labeled.
	st.Windows[0].ResetAt = now.Add(-time.Hour)
	put()
	mine, err = s.ListForMember(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "recent", mine[0].Windows[0].RangeSource)
	require.Equal(t, &MemberWindowStats{Requests: 2, Tokens: 107, AccountCostUSD: 11, UserCostUSD: 3.5}, mine[0].Windows[0].Stats)
	bounded, err := readMemberWindowStats(ctx, s.db, 1, 1, now.Add(-5*time.Hour), now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, &MemberWindowStats{Requests: 1, Tokens: 7, AccountCostUSD: 1, UserCostUSD: 0.5}, bounded, "end boundary is exclusive")
	// A pre-upgrade snapshot preserves the weekly view, not a fabricated 5h one.
	st.Windows = nil
	put()
	mine, err = s.ListForMember(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, mine[0].Windows[0].Stats)
	require.Equal(t, "recent", mine[0].Windows[0].RangeSource)
	require.Nil(t, mine[0].Windows[0].OfficialUsedPercent)
	require.NotNil(t, mine[0].Windows[1].Stats)
	st.ResetAt, st.CheckedAt = time.Time{}, time.Time{}
	put()
	mine, err = s.ListForMember(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, mine[0].Windows[1].Stats, "local usage remains available without an official window")
	require.Equal(t, "recent", mine[0].Windows[1].RangeSource)
	require.Nil(t, mine[0].Windows[1].OfficialUsedPercent, "unknown official quota must not be displayed as zero")
}
