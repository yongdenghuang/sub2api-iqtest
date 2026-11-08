//go:build integration

package carpool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type testQuota struct {
	percent float64
	reset   time.Time
	err     error
	calls   int
}

func (q *testQuota) QueryUsage(context.Context, int64) (*service.OpenAIQuotaUsage, error) {
	q.calls++
	if q.err != nil {
		return nil, q.err
	}
	return &service.OpenAIQuotaUsage{RateLimit: &service.OpenAIRateLimit{SecondaryWindow: &service.OpenAIRateLimitWindow{UsedPercent: q.percent, ResetAt: q.reset.Unix(), LimitWindowSeconds: 604800}}}, nil
}

type testInvalidator struct{ groups, subscriptions int }

func (i *testInvalidator) InvalidateCarpoolGroup(context.Context, int64) { i.groups++ }
func (i *testInvalidator) InvalidateCarpoolSubscriptions(context.Context, int64, []int64) error {
	i.subscriptions++
	return nil
}

func TestCarpoolPersistsScopedQuotasAndConfirmedResets(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("CARPOOL_TEST_DSN")
	if dsn == "" {
		c, err := tcpostgres.Run(ctx, "postgres:18-alpine", tcpostgres.WithDatabase("carpool_test"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only"), tcpostgres.BasicWaitStrategies())
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Terminate(ctx) })
		dsn, err = c.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var database string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&database))
	require.True(t, strings.HasPrefix(database, "carpool_test"), "integration tests require an isolated carpool_test database")
	_, err = db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,status TEXT DEFAULT 'active',platform TEXT DEFAULT 'openai',subscription_type TEXT DEFAULT 'subscription',weekly_limit_usd NUMERIC(20,8) DEFAULT 600,rate_multiplier NUMERIC DEFAULT 1,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 CREATE TABLE accounts(id BIGINT PRIMARY KEY,name TEXT,platform TEXT DEFAULT 'openai',type TEXT DEFAULT 'oauth',parent_account_id BIGINT,deleted_at TIMESTAMPTZ,status TEXT DEFAULT 'active',schedulable BOOLEAN DEFAULT true,credentials JSONB DEFAULT '{"token":"unchanged"}');
 CREATE TABLE account_groups(account_id BIGINT,group_id BIGINT);
 CREATE TABLE users(id BIGINT PRIMARY KEY,username TEXT,email TEXT,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ,balance NUMERIC DEFAULT 10000);
 CREATE TABLE user_subscriptions(id BIGINT PRIMARY KEY,user_id BIGINT,group_id BIGINT,status TEXT DEFAULT 'active',starts_at TIMESTAMPTZ DEFAULT now()-interval '10 days',expires_at TIMESTAMPTZ DEFAULT now()+interval '30 days',deleted_at TIMESTAMPTZ,weekly_window_start TIMESTAMPTZ DEFAULT now()-interval '1 day',weekly_usage_usd NUMERIC(20,8) DEFAULT 100,daily_window_start TIMESTAMPTZ DEFAULT now(),monthly_window_start TIMESTAMPTZ DEFAULT now(),daily_usage_usd NUMERIC DEFAULT 10,monthly_usage_usd NUMERIC DEFAULT 1000,updated_at TIMESTAMPTZ);
 CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,account_id BIGINT,subscription_id BIGINT,actual_cost NUMERIC,total_cost NUMERIC,created_at TIMESTAMPTZ);
 INSERT INTO groups(id,name) VALUES(1,'Shared'),(2,'Other'),(3,'Duplicate source');
 UPDATE groups SET subscription_type='standard',weekly_limit_usd=50 WHERE id=2;
 INSERT INTO accounts(id,name) VALUES(1,'Source'),(2,'Other');
 INSERT INTO account_groups VALUES(1,1),(2,2),(1,3);
 INSERT INTO users(id,username,email) SELECT n,'member'||n,'test'||n||'@example.com' FROM generate_series(1,4) AS n;
 INSERT INTO user_subscriptions(id,user_id,group_id) SELECT n,n,1 FROM generate_series(1,4) AS n;
 INSERT INTO user_subscriptions(id,user_id,group_id) VALUES(10,1,2),(11,1,3);`)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	q := &testQuota{percent: 25, reset: now.Add(6 * 24 * time.Hour)}
	invalidator := &testInvalidator{}
	s := &Service{db: db, quota: q, groups: invalidator, subscriptions: invalidator}
	require.NoError(t, s.migrate(ctx))
	require.NoError(t, s.migrate(ctx))
	require.Error(t, s.Sync(ctx, 1))
	require.Zero(t, q.calls)
	require.NoError(t, s.Save(ctx, 1, Config{Enabled: true, InitialTotalUSD: 2400}))
	require.True(t, s.ManagesWeeklyWindow(1))
	require.Error(t, s.Save(ctx, 3, Config{Enabled: true, InitialTotalUSD: 600}), "one source cannot promise its capacity twice")
	require.Error(t, s.Save(ctx, 2, Config{Enabled: true, InitialTotalUSD: 600}), "standard wallet groups are excluded")
	require.NoError(t, s.Sync(ctx, 1))
	g, err := loadGroup(ctx, db, 1)
	require.NoError(t, err)
	require.Equal(t, 600.0, g.WeeklyLimit)
	require.Len(t, g.Members, 4)
	for _, m := range g.Members {
		require.True(t, m.Window.Equal(q.reset.Add(-week)))
		require.Equal(t, 100.0, m.Usage)
	}
	require.False(t, g.State.Estimated)
	require.NotZero(t, invalidator.subscriptions)
	t.Run("member read access and privacy", func(t *testing.T) { testMemberViewAccess(t, s) })
	t.Run("native monitor membership visibility", func(t *testing.T) { testMonitorVisibility(t, s) })

	// A first sample starts now, so pre-existing logs cannot inflate capacity.
	_, err = db.Exec(`INSERT INTO usage_logs(account_id,subscription_id,total_cost,actual_cost,created_at) VALUES(1,1,200,200,now())`)
	require.NoError(t, err)
	g.State.Baseline.At = now.Add(-3 * time.Minute)
	initialData, err := json.Marshal(g.State)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE carpool_groups SET state=$1 WHERE group_id=1", initialData)
	require.NoError(t, err)
	q.percent = 34
	require.NoError(t, s.Sync(ctx, 1))
	g, err = loadGroup(ctx, db, 1)
	require.NoError(t, err)
	require.True(t, g.State.Estimated)
	require.InDelta(t, 2000, g.State.Capacity, 0.001)
	require.InDelta(t, 430, g.WeeklyLimit, 0.01)

	// Account failures preserve counters/caps rather than writing zeros.
	q.err = errors.New("authorization unavailable")
	require.Error(t, s.Sync(ctx, 1))
	q.err = nil
	g, err = loadGroup(ctx, db, 1)
	require.NoError(t, err)
	require.InDelta(t, 430, g.WeeklyLimit, 0.01)
	require.Contains(t, g.State.LastError, "authorization")

	// Prepare old and new cycle costs. A confirmed natural reset subtracts
	// only the old-cycle charge and preserves a request already billed after it.
	q.reset = now.Add(week - time.Hour)
	q.percent = 1
	oldReset := now.Add(-time.Hour)
	st := State{ResetAt: oldReset, Percent: 90, Capacity: 2400, Estimated: true}
	data, err := json.Marshal(st)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE carpool_groups SET state=$1 WHERE group_id=1`, data)
	require.NoError(t, err)
	_, err = db.Exec("DELETE FROM usage_logs")
	require.NoError(t, err)
	_, err = db.Exec("UPDATE user_subscriptions SET weekly_window_start=$1,weekly_usage_usd=50 WHERE group_id=1", oldReset.Add(-week))
	require.NoError(t, err)
	for id := 1; id <= 4; id++ {
		_, err = db.Exec(`INSERT INTO usage_logs(account_id,subscription_id,total_cost,actual_cost,created_at) VALUES(1,$1,40,40,$2),(1,$1,10,10,$3)`, id, now.Add(-2*time.Hour), now.Add(-30*time.Minute))
		require.NoError(t, err)
	}
	require.NoError(t, s.Sync(ctx, 1))
	g, err = loadGroup(ctx, db, 1)
	require.NoError(t, err)
	require.Equal(t, 50.0, g.Members[0].Usage)
	st = g.State
	st.CandidateAt = now.Add(-time.Minute)
	data, err = json.Marshal(st)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE carpool_groups SET state=$1 WHERE group_id=1", data)
	require.NoError(t, err)
	require.NoError(t, s.Sync(ctx, 1))
	g, err = loadGroup(ctx, db, 1)
	require.NoError(t, err)
	for _, m := range g.Members {
		require.Equal(t, 10.0, m.Usage)
		require.True(t, m.Window.Equal(oldReset))
	}
	require.NoError(t, s.Sync(ctx, 1))
	var resets int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM carpool_events WHERE kind='official_reset'").Scan(&resets))
	require.Equal(t, 1, resets)

	// Removing a member redistributes only the unspent seed balance.
	_, err = db.Exec("UPDATE user_subscriptions SET status='suspended' WHERE id=4")
	require.NoError(t, err)
	require.NoError(t, s.Sync(ctx, 1))
	g, err = loadGroup(ctx, db, 1)
	require.NoError(t, err)
	require.Len(t, g.Members, 3)
	require.InDelta(t, 796.66, g.WeeklyLimit, 0.01)

	// These are the safety boundaries that distinguish subscription allocation
	// from a wallet top-up plugin.
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM users WHERE balance=10000").Scan(&count))
	require.Equal(t, 4, count)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM user_subscriptions WHERE daily_usage_usd=10 AND monthly_usage_usd=1000").Scan(&count))
	require.Equal(t, 6, count)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM accounts WHERE credentials->>'token'='unchanged'").Scan(&count))
	require.Equal(t, 2, count)
	var limit float64
	require.NoError(t, db.QueryRow("SELECT weekly_limit_usd FROM groups WHERE id=2").Scan(&limit))
	require.Equal(t, 50.0, limit)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs").Scan(&count))
	require.Equal(t, 8, count)

	// A changed binding pauses writes but can always be disabled.
	_, err = db.Exec("INSERT INTO account_groups VALUES(2,1)")
	require.NoError(t, err)
	require.Error(t, s.Sync(ctx, 1))
	require.NoError(t, s.Save(ctx, 1, Config{Enabled: false, InitialTotalUSD: 2400}))
	require.False(t, s.ManagesWeeklyWindow(1))
	calls := q.calls
	require.Error(t, s.Sync(ctx, 1))
	require.Equal(t, calls, q.calls)
}

func testMemberViewAccess(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	// Username imports can contain email addresses; never expose them as names.
	_, err := s.db.Exec("UPDATE users SET username=email WHERE id=2")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = s.db.Exec("UPDATE users SET username='member2' WHERE id=2") })
	groups, err := s.ListForMember(ctx, 1)
	require.NoError(t, err)
	require.Len(t, groups, 1, "unconfigured groups and wallet subscriptions are not carpools")
	g := groups[0]
	require.Equal(t, int64(1), g.ID)
	require.Len(t, g.Members, 4)
	require.Equal(t, 2400.0, g.TotalUSD)
	require.Equal(t, 400.0, g.UsedUSD)
	require.Equal(t, 2000.0, g.RemainingUSD)
	require.True(t, g.Members[0].IsSelf)
	require.False(t, g.Members[1].IsSelf)
	require.Empty(t, g.Members[1].Name)
	require.Equal(t, 600.0, g.Members[1].AllocatedUSD)
	require.Equal(t, "sampling", g.Status)
	outsider, err := s.ListForMember(ctx, 999)
	require.NoError(t, err)
	require.Empty(t, outsider)

	// Every active-membership restriction must apply both to access and roster.
	for _, tc := range []struct{ name, deny, restore string }{
		{"expired", "UPDATE user_subscriptions SET expires_at=now()-interval '1 hour' WHERE id=1", "UPDATE user_subscriptions SET expires_at=now()+interval '30 days' WHERE id=1"},
		{"future", "UPDATE user_subscriptions SET starts_at=now()+interval '1 hour' WHERE id=1", "UPDATE user_subscriptions SET starts_at=now()-interval '10 days' WHERE id=1"},
		{"suspended subscription", "UPDATE user_subscriptions SET status='suspended' WHERE id=1", "UPDATE user_subscriptions SET status='active' WHERE id=1"},
		{"deleted subscription", "UPDATE user_subscriptions SET deleted_at=now() WHERE id=1", "UPDATE user_subscriptions SET deleted_at=NULL WHERE id=1"},
		{"disabled user", "UPDATE users SET status='disabled' WHERE id=1", "UPDATE users SET status='active' WHERE id=1"},
		{"deleted user", "UPDATE users SET deleted_at=now() WHERE id=1", "UPDATE users SET deleted_at=NULL WHERE id=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.db.Exec(tc.deny)
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = s.db.Exec(tc.restore) })
			mine, err := s.ListForMember(ctx, 1)
			require.NoError(t, err)
			require.Empty(t, mine)
			other, err := s.ListForMember(ctx, 3)
			require.NoError(t, err)
			require.Len(t, other, 1)
			require.Len(t, other[0].Members, 3)
			require.Equal(t, "adjusting", other[0].Status)
		})
	}
	for _, tc := range []struct{ deny, restore string }{
		{"UPDATE carpool_groups SET enabled=false WHERE group_id=1", "UPDATE carpool_groups SET enabled=true WHERE group_id=1"},
		{"UPDATE groups SET status='inactive' WHERE id=1", "UPDATE groups SET status='active' WHERE id=1"},
		{"UPDATE groups SET deleted_at=now() WHERE id=1", "UPDATE groups SET deleted_at=NULL WHERE id=1"},
	} {
		_, err = s.db.Exec(tc.deny)
		require.NoError(t, err)
		mine, readErr := s.ListForMember(ctx, 1)
		_, err = s.db.Exec(tc.restore)
		require.NoError(t, err)
		require.NoError(t, readErr)
		require.Empty(t, mine)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	s.RegisterMemberRoutes(r.Group("/api/v1"), func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/carpool?user_id=999&group_id=3", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var body struct {
		Data []MemberGroupView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, int64(1), body.Data[0].ID, "request parameters cannot override the authenticated viewer")
	for _, private := range []string{"@example.com", "Source", "credentials", "account_id", "user_id", "balance", "last_error", "baseline"} {
		require.NotContains(t, w.Body.String(), private)
	}
	require.Equal(t, 1, s.quota.(*testQuota).calls, "page loads must not call upstream")
	var sum float64
	require.NoError(t, s.db.QueryRow("SELECT sum(weekly_usage_usd) FROM user_subscriptions WHERE group_id=1").Scan(&sum))
	require.Equal(t, 400.0, sum, "reading must preserve usage counters")
}
