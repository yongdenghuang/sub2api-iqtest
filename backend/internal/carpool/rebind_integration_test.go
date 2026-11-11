//go:build integration

package carpool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testUpstreamReplacement(t *testing.T, db *sql.DB) {
	for index, mode := range []string{AllocationAuto, AllocationManual} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			id := int64(201 + index*10)
			next := id + 1
			now := time.Now().UTC().Truncate(time.Second)
			q := &testQuota{percent: 38, reset: now.Add(6 * 24 * time.Hour)}
			invalidator := &testInvalidator{}
			s := &Service{db: db, quota: q, groups: invalidator, subscriptions: invalidator}
			exec := func(query string, args ...any) {
				t.Helper()
				_, err := db.ExecContext(ctx, query, args...)
				require.NoError(t, err)
			}
			exec("INSERT INTO groups(id,name) VALUES($1,'Same car')", id)
			exec("INSERT INTO accounts(id,name) VALUES($1,'Old source'),($2,'New source')", id, next)
			exec("INSERT INTO account_groups VALUES($1,$1)", id)
			exec("INSERT INTO user_subscriptions(id,user_id,group_id,weekly_window_start) VALUES($1,1,$1,$2)", id, now.Add(-3*24*time.Hour))
			t.Cleanup(func() {
				exec("DELETE FROM usage_logs WHERE subscription_id=$1", id)
				exec("DELETE FROM carpool_events WHERE group_id=$1", id)
				exec("DELETE FROM carpool_groups WHERE group_id=$1", id)
				exec("DELETE FROM user_subscriptions WHERE group_id=$1", id)
				exec("DELETE FROM account_groups WHERE group_id=$1", id)
				exec("DELETE FROM accounts WHERE id IN ($1,$2)", id, next)
				exec("DELETE FROM groups WHERE id=$1", id)
			})
			group := func() Group { t.Helper(); g, err := loadGroup(ctx, db, id); require.NoError(t, err); return g }
			putState := func(st State) {
				t.Helper()
				raw, err := json.Marshal(st)
				require.NoError(t, err)
				exec("UPDATE carpool_groups SET state=$2 WHERE group_id=$1", id, raw)
			}
			counters := func() string {
				t.Helper()
				var raw string
				require.NoError(t, db.QueryRow("SELECT row_to_json(s)::text FROM user_subscriptions s WHERE id=$1", id).Scan(&raw))
				return raw
			}
			on := true
			cfg := Config{Enabled: true, AllocationMode: mode, InitialTotalUSD: 2400, ManualWeeklyUSD: 600, SyncOfficialReset: &on}
			require.NoError(t, s.Save(ctx, id, cfg))
			oldState := State{Estimated: true, Capacity: 1034, Percent: 90, ResetAt: now.Add(time.Hour), CheckedAt: now, Baseline: Observation{At: now, Cost: 200}, Candidate: now.Add(week), LastError: "old failure"}
			putState(oldState)
			exec("UPDATE account_groups SET account_id=$2 WHERE group_id=$1", id, next)
			require.True(t, group().BindingChanged)
			require.Equal(t, "Old source", group().BoundAccountName)
			before := counters()
			var conflict *BindingConflict
			require.ErrorAs(t, s.Save(ctx, id, cfg), &conflict)
			require.Equal(t, oldState, group().State, "unconfirmed save cannot mutate the old samples")
			require.ErrorAs(t, s.Sync(ctx, id), &conflict)
			require.Zero(t, q.calls, "background sync must not query the new source before confirmation")
			req := SaveRequest{Config: cfg, ReplaceUpstream: &UpstreamReplacement{PreviousAccountID: id, AccountID: next}}
			// A changed source while the dialog is open invalidates its confirmation.
			stale := req
			stale.ReplaceUpstream = &UpstreamReplacement{PreviousAccountID: id, AccountID: next + 1}
			require.ErrorAs(t, s.save(ctx, id, stale), &conflict)
			// The existing unique-account guard still rolls back the entire save.
			exec("UPDATE account_groups SET account_id=1 WHERE group_id=$1", id)
			duplicate := req
			duplicate.ManualWeeklyUSD = 999
			duplicate.ReplaceUpstream = &UpstreamReplacement{PreviousAccountID: id, AccountID: 1}
			require.Error(t, s.save(ctx, id, duplicate))
			require.Equal(t, id, group().BoundAccountID)
			require.Equal(t, 600.0, group().WeeklyLimit)
			exec("UPDATE account_groups SET account_id=$2 WHERE group_id=$1", id, next)
			// Check the actual HTTP contract, not only the service method.
			router := gin.New()
			s.RegisterRoutes(router.Group("/api/v1"))
			body, err := json.Marshal(cfg)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/carpool/groups/"+strconv.FormatInt(id, 10), strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, http.StatusConflict, w.Code)
			require.Contains(t, w.Body.String(), "CARPOOL_BINDING_CHANGED")
			body, err = json.Marshal(req)
			require.NoError(t, err)
			request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/carpool/groups/"+strconv.FormatInt(id, 10), strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			w = httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			g := group()
			require.Equal(t, next, g.BoundAccountID)
			require.False(t, g.BindingChanged)
			require.Equal(t, before, counters())
			require.Zero(t, g.State.CheckedAt)
			require.Zero(t, g.State.ResetAt)
			require.Zero(t, g.State.Candidate)
			require.Zero(t, g.State.Baseline)
			require.Empty(t, g.State.LastError)
			require.False(t, g.State.Estimated)
			require.True(t, g.State.KeepUsageUntilReset)
			if mode == AllocationAuto {
				require.Equal(t, 2400.0, g.State.Capacity)
			}
			var event struct {
				PreviousAccountID int64 `json:"previous_account_id"`
				AccountID         int64 `json:"account_id"`
				PreviousState     State `json:"previous_state"`
			}
			var raw []byte
			require.NoError(t, db.QueryRow("SELECT detail FROM carpool_events WHERE group_id=$1 AND kind='upstream_replaced'", id).Scan(&raw))
			require.NoError(t, json.Unmarshal(raw, &event))
			require.Equal(t, id, event.PreviousAccountID)
			require.Equal(t, next, event.AccountID)
			require.Equal(t, oldState.Capacity, event.PreviousState.Capacity)
			q.err = errors.New("new source temporarily unavailable")
			require.Error(t, s.Sync(ctx, id))
			require.Equal(t, before, counters())
			q.err = nil
			// Logs on both sides of the new source's initial anchor must not clear
			// any spent quota until its NEXT confirmed reset.
			exec("INSERT INTO usage_logs(account_id,subscription_id,total_cost,actual_cost,created_at) VALUES($1,$1,50,50,$3),($2,$1,50,50,$4)", id, next, now.Add(-2*24*time.Hour), now.Add(-time.Hour))
			require.NoError(t, s.Sync(ctx, id))
			require.NoError(t, s.Sync(ctx, id))
			require.Equal(t, before, counters(), "first and subsequent same-window syncs preserve ALL counters/anchors")
			observed := group().State
			require.ErrorAs(t, s.save(ctx, id, req), &conflict, "replayed confirmation must not reset fresh samples")
			require.Equal(t, observed, group().State)
			q.reset, q.percent = now.Add(week-time.Minute), 1
			exec("UPDATE user_subscriptions SET weekly_usage_usd=110 WHERE id=$1", id)
			exec("INSERT INTO usage_logs(account_id,subscription_id,total_cost,actual_cost,created_at) VALUES($2,$1,10,10,$3)", id, next, now)
			require.NoError(t, s.Sync(ctx, id))
			require.Equal(t, 110.0, group().Members[0].Usage)
			st := group().State
			st.CandidateAt = now.Add(-time.Minute)
			putState(st)
			require.NoError(t, s.Sync(ctx, id))
			require.False(t, group().State.KeepUsageUntilReset)
			require.Equal(t, 10.0, group().Members[0].Usage, "next reset removes proven old charges and keeps new-period billing")
			after := counters()
			require.NoError(t, s.Sync(ctx, id))
			require.Equal(t, after, counters())
			require.Positive(t, invalidator.groups)
			require.Positive(t, invalidator.subscriptions)
		})
	}
}
