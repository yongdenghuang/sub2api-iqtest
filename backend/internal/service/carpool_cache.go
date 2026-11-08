package service

import "context"

type carpoolWindowPolicy struct{ manages func(int64) bool }

func (s *SubscriptionService) SetCarpoolWeeklyController(manages func(int64) bool) {
	s.carpoolPolicy.Store(&carpoolWindowPolicy{manages: manages})
}

func (s *SubscriptionService) carpoolManagesWeekly(groupID int64) bool {
	p := s.carpoolPolicy.Load()
	return p != nil && p.manages != nil && p.manages(groupID)
}

// InvalidateCarpoolGroup refreshes embedded group limits in API-key auth
// snapshots after the carpool transaction has committed.
func (s *adminServiceImpl) InvalidateCarpoolGroup(ctx context.Context, groupID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, groupID)
	}
}

// InvalidateCarpoolSubscriptions clears local and remote subscription caches.
func (s *SubscriptionService) InvalidateCarpoolSubscriptions(_ context.Context, groupID int64, userIDs []int64) error {
	for _, id := range userIDs {
		if err := s.invalidateSubscriptionCaches(id, groupID); err != nil {
			return err
		}
	}
	return nil
}
