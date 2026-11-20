//go:build integration

package carpool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testDirectPreview(t *testing.T, s *Service) {
	ctx := context.Background()
	_, err := s.db.Exec(`
 INSERT INTO groups(id,name,weekly_limit_usd) VALUES(131,'四人车ujjw',480);
 INSERT INTO accounts(id,name) VALUES(131,'Preview source');
 INSERT INTO account_groups VALUES (131,131);
 INSERT INTO user_subscriptions(id,user_id,group_id) SELECT 130+n,n,131 FROM generate_series(1,3) AS n;
 INSERT INTO carpool_groups(group_id,account_id,enabled,initial_total_usd,allocation_mode)
 VALUES(131,131,true,2400,'manual');`)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(5 * 24 * time.Hour)
	st := State{
		CheckedAt: now, ResetAt: reset, Percent: 50,
		Windows: []OfficialWindow{{Window: "7d", UsedPercent: 50, ResetAt: reset}},
	}
	body, err := json.Marshal(st)
	require.NoError(t, err)
	_, err = s.db.Exec("UPDATE carpool_groups SET state=$1 WHERE group_id=131", string(body))
	require.NoError(t, err)
	_, err = s.db.Exec(`
 INSERT INTO usage_logs(account_id,subscription_id,group_id,total_cost,actual_cost,created_at) VALUES
 (131,131,131,300,999,$1),(131,132,131,200,200,$1),(131,133,131,100,100,$1),
 (131,NULL,2,300,300,$1),(2,NULL,131,500,500,$1),
 (131,131,131,999,999,$2),(131,131,131,999,999,$3)`, now.Add(-time.Minute), reset.Add(-week-time.Second), now)
	require.NoError(t, err)

	p, err := s.directPreview(ctx, 131, 2400)
	require.NoError(t, err)
	require.Equal(t, 900.0, p.Site)
	require.Equal(t, 300.0, *p.Direct)
	require.Equal(t, 525.0, *p.Suggested)
	p, err = s.directPreview(ctx, 131, 1000)
	require.NoError(t, err)
	require.Nil(t, p.Direct)
	require.Nil(t, p.Suggested)

	g, err := loadGroup(ctx, s.db, 131)
	require.NoError(t, err)
	require.Equal(t, 480.0, g.WeeklyLimit)
	require.Equal(t, 100.0, g.Members[0].Usage)
	require.Equal(t, st.CheckedAt, g.State.CheckedAt)
}
