//go:build integration

package carpool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testManualQuotaAndIndependentReset(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	q := &testQuota{percent: 38, reset: now.Add(6 * 24 * time.Hour)}
	invalidator := &testInvalidator{}
	s := &Service{db: db, quota: q, groups: invalidator, subscriptions: invalidator}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO groups(id,name,weekly_limit_usd) VALUES(101,'Manual test',292.30);
 INSERT INTO accounts(id,name) VALUES(101,'Manual source');
 INSERT INTO account_groups VALUES(101,101);
 INSERT INTO user_subscriptions(id,user_id,group_id) SELECT 100+n,n,101 FROM generate_series(1,4) n;`)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM usage_logs WHERE account_id=101;
 DELETE FROM carpool_events WHERE group_id=101;
 DELETE FROM carpool_groups WHERE group_id=101;
 DELETE FROM user_subscriptions WHERE group_id=101;
 DELETE FROM account_groups WHERE group_id=101;
 DELETE FROM accounts WHERE id=101;
 DELETE FROM groups WHERE id=101;`)
		require.NoError(t, err)
	})
	group := func() Group { t.Helper(); g, err := loadGroup(ctx, db, 101); require.NoError(t, err); return g }
	counters := func() string {
		t.Helper()
		var raw string
		err := db.QueryRow(`SELECT json_agg(t ORDER BY t.id)::text FROM (SELECT id,weekly_usage_usd,weekly_window_start,daily_usage_usd,daily_window_start,monthly_usage_usd,monthly_window_start,expires_at FROM user_subscriptions WHERE group_id=101) t`).Scan(&raw)
		require.NoError(t, err)
		return raw
	}
	putState := func(st State) {
		t.Helper()
		raw, err := json.Marshal(st)
		require.NoError(t, err)
		exec("UPDATE carpool_groups SET state=$1 WHERE group_id=101", raw)
	}
	on, off := true, false
	manual := Config{Enabled: true, AllocationMode: AllocationManual, ManualWeeklyUSD: 600, InitialTotalUSD: 2400, SyncOfficialReset: &on}
	require.NoError(t, s.Save(ctx, 101, Config{Enabled: true, InitialTotalUSD: 2400}))
	require.NoError(t, s.migrate(ctx))
	require.Equal(t, AllocationAuto, group().Config.AllocationMode, "old configurations retain automatic allocation")
	putState(State{ResetAt: q.reset, CheckedAt: now, Estimated: true, Capacity: 1000, Percent: 38, Members: 4})
	before := counters()
	calls := q.calls
	require.NoError(t, s.Save(ctx, 101, manual))
	require.Equal(t, calls, q.calls, "manual save must not require upstream access")
	require.Equal(t, before, counters(), "saving a manual cap never resets any counter or window")
	require.Equal(t, 600.0, group().WeeklyLimit)
	require.False(t, group().State.Estimated)
	require.True(t, s.ManagesWeeklyWindow(101))
	// Even a free group can use fixed allowances: no dollar estimation is needed.
	exec("UPDATE groups SET rate_multiplier=0 WHERE id=101")
	q.percent = 85
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, 600.0, group().WeeklyLimit, "off-site consumption must not lower a manual cap")
	aligned := counters()
	exec("UPDATE user_subscriptions SET status='suspended' WHERE id=104")
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, 600.0, group().WeeklyLimit, "membership changes must not change per-person manual cap")
	mine, err := s.ListForMember(ctx, 1)
	require.NoError(t, err)
	var memberView MemberGroupView
	for _, v := range mine {
		if v.ID == 101 {
			memberView = v
		}
	}
	require.Equal(t, AllocationManual, memberView.AllocationMode)
	require.Equal(t, 1800.0, memberView.TotalUSD)
	require.Equal(t, 300.0, memberView.UsedUSD)
	require.Equal(t, 1500.0, memberView.RemainingUSD)
	require.Nil(t, memberView.UpstreamCapacityUSD)
	require.True(t, memberView.SyncOfficialReset)
	require.Equal(t, "synced", memberView.Status)
	exec("UPDATE user_subscriptions SET status='active' WHERE id=104")
	exec("UPDATE groups SET weekly_limit_usd=650 WHERE id=101")
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, 650.0, group().Config.ManualWeeklyUSD, "native group edits remain the source of truth")
	require.Equal(t, aligned, counters())
	q.err = errors.New("upstream unavailable")
	manual.ManualWeeklyUSD = 700
	require.NoError(t, s.Save(ctx, 101, manual))
	require.Error(t, s.Sync(ctx, 101))
	require.Equal(t, 700.0, group().WeeklyLimit)
	require.Equal(t, aligned, counters(), "upstream failure cannot alter saved manual budgets or usage")
	q.err = nil
	manual.ManualWeeklyUSD = 600
	manual.SyncOfficialReset = &off
	require.NoError(t, s.Save(ctx, 101, manual))
	require.NoError(t, s.refreshManaged(ctx))
	require.False(t, s.ManagesWeeklyWindow(101))
	// A confirmed upstream reset with synchronization OFF must not touch counters.
	oldReset := now.Add(-time.Hour)
	q.reset = oldReset.Add(week)
	q.percent = 1
	putState(State{ResetAt: oldReset, Percent: 90})
	exec("UPDATE user_subscriptions SET weekly_usage_usd=50,weekly_window_start=$1 WHERE group_id=101", oldReset.Add(-week))
	for id := 101; id <= 104; id++ {
		exec(`INSERT INTO usage_logs(account_id,subscription_id,total_cost,actual_cost,created_at) VALUES(101,$1,40,40,$2),(101,$1,10,10,$3)`, id, now.Add(-2*time.Hour), now.Add(-30*time.Minute))
	}
	before = counters()
	require.NoError(t, s.Sync(ctx, 101))
	st := group().State
	st.CandidateAt = now.Add(-time.Minute)
	putState(st)
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, before, counters())
	require.Equal(t, 600.0, group().WeeklyLimit)
	require.True(t, group().State.ResetAt.Equal(q.reset), "official display still follows confirmed resets")
	// Enabling reset sync only changes counters on the next confirmed sync.
	manual.SyncOfficialReset = &on
	require.NoError(t, s.Save(ctx, 101, manual))
	require.Equal(t, before, counters())
	require.NoError(t, s.Sync(ctx, 101))
	for _, m := range group().Members {
		require.Equal(t, 10.0, m.Usage, "retain already-billed consumption in the new upstream window")
		require.True(t, m.Window.Equal(oldReset))
	}
	require.Equal(t, 600.0, group().WeeklyLimit)
	afterReset := counters()
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, afterReset, counters(), "same upstream window cannot clear usage twice")
	var resetEvents int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM carpool_events WHERE group_id=101 AND kind='official_reset'").Scan(&resetEvents))
	require.Zero(t, resetEvents, "observation-only reset must not claim it reset subscribers")
	// Going back to auto uses new samples, without carrying the distorted estimate.
	exec("UPDATE groups SET rate_multiplier=1 WHERE id=101")
	require.NoError(t, s.Save(ctx, 101, Config{Enabled: true, AllocationMode: AllocationAuto, InitialTotalUSD: 2400}))
	require.Equal(t, afterReset, counters())
	require.False(t, group().State.Estimated)
	require.Zero(t, group().State.Baseline)
	require.Equal(t, 2400.0, group().State.Capacity)
	require.NoError(t, s.Sync(ctx, 101))
	require.Equal(t, 600.0, group().WeeklyLimit)
	require.Equal(t, afterReset, counters())
}
