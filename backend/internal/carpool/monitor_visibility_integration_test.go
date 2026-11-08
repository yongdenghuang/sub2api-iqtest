//go:build integration

package carpool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func testMonitorVisibility(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	_, err := s.db.Exec(`CREATE TABLE channel_monitors(id BIGINT PRIMARY KEY,account_id BIGINT,enabled BOOLEAN DEFAULT true);
 INSERT INTO groups(id,name) VALUES(4,'Another car');
 INSERT INTO accounts(id,name) VALUES(3,'Another source');
 INSERT INTO account_groups VALUES(3,4);
 INSERT INTO user_subscriptions(id,user_id,group_id) VALUES(20,1,4);
 INSERT INTO channel_monitors(id,account_id,enabled) VALUES(1,1,true),(2,2,true),(3,NULL,true),(4,1,false),(5,3,true);`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = s.db.Exec(`DROP TABLE channel_monitors; DELETE FROM user_subscriptions WHERE id=20;
 DELETE FROM account_groups WHERE group_id=4; DELETE FROM groups WHERE id=4; DELETE FROM accounts WHERE id=3;`)
	})
	read := func(id int64, want map[int64]bool) {
		t.Helper()
		got, err := s.VisibleMonitorIDs(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	read(1, map[int64]bool{1: true, 5: true})
	read(2, map[int64]bool{1: true})
	read(999, map[int64]bool{})
	read(0, map[int64]bool{})
	for _, tc := range []struct{ name, deny, restore string }{
		{"expired", "UPDATE user_subscriptions SET expires_at=now()-interval '1 hour' WHERE id=2", "UPDATE user_subscriptions SET expires_at=now()+interval '30 days' WHERE id=2"},
		{"future", "UPDATE user_subscriptions SET starts_at=now()+interval '1 hour' WHERE id=2", "UPDATE user_subscriptions SET starts_at=now()-interval '10 days' WHERE id=2"},
		{"suspended subscription", "UPDATE user_subscriptions SET status='suspended' WHERE id=2", "UPDATE user_subscriptions SET status='active' WHERE id=2"},
		{"deleted subscription", "UPDATE user_subscriptions SET deleted_at=now() WHERE id=2", "UPDATE user_subscriptions SET deleted_at=NULL WHERE id=2"},
		{"disabled user", "UPDATE users SET status='disabled' WHERE id=2", "UPDATE users SET status='active' WHERE id=2"},
		{"deleted user", "UPDATE users SET deleted_at=now() WHERE id=2", "UPDATE users SET deleted_at=NULL WHERE id=2"},
		{"disabled group", "UPDATE groups SET status='inactive' WHERE id=1", "UPDATE groups SET status='active' WHERE id=1"},
		{"deleted group", "UPDATE groups SET deleted_at=now() WHERE id=1", "UPDATE groups SET deleted_at=NULL WHERE id=1"},
		{"deleted account", "UPDATE accounts SET deleted_at=now() WHERE id=1", "UPDATE accounts SET deleted_at=NULL WHERE id=1"},
		{"unlinked source", "DELETE FROM account_groups WHERE account_id=1 AND group_id=1", "INSERT INTO account_groups VALUES(1,1)"},
		{"disabled monitor", "UPDATE channel_monitors SET enabled=false WHERE id=1", "UPDATE channel_monitors SET enabled=true WHERE id=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.db.Exec(tc.deny)
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = s.db.Exec(tc.restore) })
			read(2, map[int64]bool{})
		})
	}
	// Failed upstream accounts still need their availability displayed; turning
	// off dynamic sharing must not hide a fixed-budget car's monitoring either.
	_, err = s.db.Exec("UPDATE accounts SET status='error' WHERE id=1; UPDATE carpool_groups SET enabled=false WHERE group_id=1")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = s.db.Exec("UPDATE accounts SET status='active' WHERE id=1; UPDATE carpool_groups SET enabled=true WHERE group_id=1")
	})
	read(2, map[int64]bool{1: true})
}
