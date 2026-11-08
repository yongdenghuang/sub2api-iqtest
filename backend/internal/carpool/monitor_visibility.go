package carpool

import "context"

// VisibleMonitorIDs limits native quota/probe cards to the viewer's current
// subscription groups. GroupName is only a label and never grants access.
// This does not depend on automatic budget sharing being enabled: fixed-budget
// cars also need monitoring. Unlinked/disabled cards and deleted source accounts
// stay hidden; failing upstream accounts still need their status displayed.
func (s *Service) VisibleMonitorIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	allowed := map[int64]bool{}
	if userID <= 0 {
		return allowed, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT cm.id
 FROM channel_monitors cm
 JOIN accounts a ON a.id=cm.account_id AND a.deleted_at IS NULL
 JOIN account_groups ag ON ag.account_id=a.id
 JOIN groups g ON g.id=ag.group_id
 JOIN user_subscriptions s ON s.group_id=g.id
 JOIN users u ON u.id=s.user_id
 WHERE cm.enabled AND g.deleted_at IS NULL AND g.status='active'
 AND g.subscription_type='subscription' AND s.user_id=$1
 AND s.deleted_at IS NULL AND s.status='active' AND s.starts_at<=now() AND s.expires_at>now()
 AND u.deleted_at IS NULL AND u.status='active'`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		allowed[id] = true
	}
	return allowed, rows.Err()
}
