package iqtest

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// QuotaRecovery intentionally cannot consume or automatically redeem cards.
type QuotaRecovery interface {
	RefreshUsage(context.Context, int64) (*service.OpenAIQuotaUsage, error)
}

type quotaBlockedCheck struct {
	AccountID int64
	PlanID    int64
	Kind      TestKind
}

func quotaBlocked(st AccountState) bool {
	if st.LastOutcome != OutcomeInfraError && st.LastOutcome != OutcomeSkipped {
		return false
	}
	return st.LastReason == ReasonQuotaExhausted || st.LastReason == ReasonRateLimited || st.LastReason == ReasonCoolingDown
}

// Poll only blocked OpenAI OAuth accounts. Sorting by last attempt prevents a
// slow/broken account from starving other accounts when the tick times out.
func (s *Store) quotaRecoveryCandidates(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id FROM accounts a
	 WHERE a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth'
	 AND a.parent_account_id IS NULL
	 AND a.status='active' AND a.schedulable=true
	 AND (NOT a.auto_pause_on_expired OR a.expires_at IS NULL OR a.expires_at > now())
	 AND (a.rate_limit_reset_at > now() OR EXISTS (
	  SELECT 1 FROM iq_account_state st LEFT JOIN iq_plans p ON p.id=st.plan_id
	  WHERE st.account_id=a.id AND (st.plan_id=0 OR p.enabled)
	  AND st.state->>'last_outcome' IN ('infra_error','skipped')
	  AND st.state->>'last_reason' IN ('quota_exhausted','rate_limited','cooling_down')
	 )) ORDER BY a.extra->>'iqtest_quota_checked_at' NULLS FIRST, a.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) quotaBlockedChecks(ctx context.Context, accountID int64) ([]quotaBlockedCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT st.account_id, st.plan_id, st.kind FROM iq_account_state st
	 LEFT JOIN iq_plans p ON p.id=st.plan_id
	 WHERE st.account_id=$1 AND (st.plan_id=0 OR p.enabled)
	 AND st.state->>'last_outcome' IN ('infra_error','skipped')
	 AND st.state->>'last_reason' IN ('quota_exhausted','rate_limited','cooling_down')
	 AND NOT EXISTS (
	  SELECT 1 FROM iq_results r JOIN iq_runs run ON run.id=r.run_id
	  WHERE r.account_id=st.account_id AND r.kind=st.kind AND run.trigger='quota_recovery'
	  AND r.created_at > now() - interval '5 minutes'
	 ) ORDER BY st.plan_id, st.kind`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var checks []quotaBlockedCheck
	for rows.Next() {
		var c quotaBlockedCheck
		if err := rows.Scan(&c.AccountID, &c.PlanID, &c.Kind); err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// recoverQuota is independent of a plan's next_run_at, so an external reset is
// noticed even with the panel closed and a multi-day old 429 cooldown present.
func (s *Scheduler) recoverQuota() {
	if s.quota == nil || !s.recoveryMu.TryLock() {
		return
	}
	defer s.recoveryMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	release, ok := s.lock(ctx, -1)
	if !ok {
		return
	}
	defer release()
	ids, err := s.store.quotaRecoveryCandidates(ctx)
	if err != nil {
		slog.Warn("iqtest: quota recovery scan failed", "error", err)
		return
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	var pending []quotaBlockedCheck
loop:
	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()
			accounts, err := s.engine.accounts.GetAccountsByIDs(ctx, []int64{id})
			if err != nil || len(accounts) != 1 || accounts[0] == nil || accounts[0].IsShadow() {
				return
			}
			usage, err := s.quota.RefreshUsage(ctx, id)
			if err != nil {
				slog.Warn("iqtest: quota probe failed", "account_id", id, "error", err)
				return
			}
			if !service.OpenAIQuotaAvailable(usage) {
				return
			}
			// Re-read after the conditional clear; a concurrent 429 must still win.
			accounts, err = s.engine.accounts.GetAccountsByIDs(ctx, []int64{id})
			if err != nil || len(accounts) != 1 || accounts[0] == nil || !accounts[0].Schedulable || !CheckEligibility(accounts[0], time.Now()).OK {
				return
			}
			checks, err := s.store.quotaBlockedChecks(ctx, id)
			if err != nil {
				return
			}
			mu.Lock()
			pending = append(pending, checks...)
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	// A slow probe must not prevent already-confirmed recoveries from launching.
	launchCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	s.launchQuotaRecovery(launchCtx, pending)
}

func (s *Scheduler) launchQuotaRecovery(ctx context.Context, pending []quotaBlockedCheck) {
	byPlan := map[int64]map[int64]map[TestKind]bool{}
	for _, check := range pending {
		if byPlan[check.PlanID] == nil {
			byPlan[check.PlanID] = map[int64]map[TestKind]bool{}
		}
		if byPlan[check.PlanID][check.AccountID] == nil {
			byPlan[check.PlanID][check.AccountID] = map[TestKind]bool{}
		}
		byPlan[check.PlanID][check.AccountID][check.Kind] = true
	}
	for planID, checks := range byPlan {
		p := &Plan{Name: "额度恢复补测", ScopeType: ScopeAccounts, Checks: []CheckConfig{{Kind: TestCandy, Model: AutoModel}, {Kind: TestPelicanSVG, Model: AutoModel}}}
		if planID != 0 {
			var err error
			p, err = s.store.GetPlan(ctx, planID)
			if err != nil || !p.Enabled {
				continue
			}
			// Honor edits to the original plan's scope after the failed test.
			accounts, err := s.engine.ResolveAccounts(ctx, p)
			if err != nil {
				continue
			}
			allowed := map[int64]bool{}
			for _, a := range accounts {
				allowed[a.ID] = true
			}
			for id := range checks {
				if !allowed[id] {
					delete(checks, id)
				}
			}
		}
		// Editing a plan may remove one test while leaving the account in scope.
		// Do not create empty recovery runs for that historical test forever.
		configured := map[TestKind]bool{}
		for _, check := range p.Checks {
			configured[check.Kind] = true
		}
		for id, kinds := range checks {
			for kind := range kinds {
				if !configured[kind] {
					delete(kinds, kind)
				}
			}
			if len(kinds) == 0 {
				delete(checks, id)
			}
		}
		clone := *p
		clone.ScopeType, clone.GroupIDs, clone.AccountIDs = ScopeAccounts, nil, nil
		for id := range checks {
			clone.AccountIDs = append(clone.AccountIDs, id)
		}
		if len(clone.AccountIDs) == 0 {
			continue
		}
		sort.Slice(clone.AccountIDs, func(i, j int) bool { return clone.AccountIDs[i] < clone.AccountIDs[j] })
		if err := clone.Normalize(); err != nil {
			continue
		}
		runID, err := s.startFiltered(ctx, &clone, "quota_recovery", checks)
		if err != nil && !errors.Is(err, ErrAlreadyRunning) {
			slog.Warn("iqtest: quota recovery run failed", "plan_id", planID, "error", err)
		} else if err == nil {
			slog.Info("iqtest: quota recovery run started", "plan_id", planID, "run_id", runID, "accounts", clone.AccountIDs)
		}
	}
}
