package iqtest

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQuotaRecoveryOnlyRetestsBlockedChecks(t *testing.T) {
	repo := newMemRepo()
	var called []int64
	var mu sync.Mutex
	e := newTestEngine(repo, []*service.Account{acct(1), acct(2)}, reply("21", &called, &mu))
	p := newPlan()
	p.Checks = []CheckConfig{{Kind: TestCandy}, {Kind: TestPelicanSVG}}
	repo.states[skey(1, TestCandy)] = &StateRow{AccountState: AccountState{LastOutcome: OutcomeInfraError, LastReason: ReasonQuotaExhausted}}
	repo.states[skey(1, TestPelicanSVG)] = &StateRow{AccountState: AccountState{LastOutcome: OutcomePass}}
	// Account 2 has already completed a normal test since the monitor queued it.
	repo.states[skey(2, TestCandy)] = &StateRow{AccountState: AccountState{LastOutcome: OutcomePass}}
	pr, err := e.Prepare(context.Background(), p, "quota_recovery")
	require.NoError(t, err)
	pr.RecoveryChecks = map[int64]map[TestKind]bool{1: {TestCandy: true}, 2: {TestCandy: true}}
	stats := e.Execute(context.Background(), pr)
	require.Equal(t, []int64{1}, called)
	require.Equal(t, 1, stats.Requests)
	require.Equal(t, 1, stats.Pass)
	require.Len(t, repo.results, 1)
	require.Equal(t, TestCandy, repo.results[0].Kind)
}

func TestQuotaRecoveryHonorsPlanChangesAndSchedule(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		ids      []int64
		wantRuns int
	}{
		{"disabled plan", false, []int64{1}, 0},
		{"account removed from scope", true, []int64{2}, 0},
		{"only recovered account", true, []int64{1, 2}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			p := newPlan()
			p.Enabled = tc.enabled
			p.ScopeType = ScopeAccounts
			p.AccountIDs = tc.ids
			next := time.Now().Add(24 * time.Hour)
			cfg, err := json.Marshal(p)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT .* FROM iq_plans WHERE id").WithArgs(int64(1)).WillReturnRows(
				sqlmock.NewRows(strings.Split(planCols, ", ")).AddRow(1, p.Name, p.Enabled, cfg, next, nil, time.Now(), time.Now()))
			repo := newMemRepo()
			repo.states[skey(1, TestCandy)] = &StateRow{AccountState: AccountState{LastOutcome: OutcomeInfraError, LastReason: ReasonQuotaExhausted}}
			var called []int64
			var mu sync.Mutex
			e := newTestEngine(repo, []*service.Account{acct(1), acct(2)}, reply("21", &called, &mu))
			s := NewScheduler(&Store{db: db}, e, nil, time.UTC)
			s.launchQuotaRecovery(context.Background(), []quotaBlockedCheck{{AccountID: 1, PlanID: 1, Kind: TestCandy}})
			require.Eventually(t, func() bool { _, running := s.running.Load(int64(1)); return !running }, time.Second, 5*time.Millisecond)
			repo.mu.Lock()
			runs := repo.runs
			repo.mu.Unlock()
			require.Equal(t, tc.wantRuns, runs)
			if tc.wantRuns > 0 {
				mu.Lock()
				require.Equal(t, []int64{1}, called)
				mu.Unlock()
			}
			// No UPDATE of next_run_at or last_run_at is expected by sqlmock.
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestQuotaRecoveryDoesNotRetryAuthOrQualityFailures(t *testing.T) {
	for _, st := range []AccountState{
		{LastOutcome: OutcomeInfraError, LastReason: ReasonAuthExpired},
		{LastOutcome: OutcomeInfraError, LastReason: ReasonModelUnsupported},
		{LastOutcome: OutcomeQualityFail, LastReason: ReasonAnswerMismatch},
		{LastOutcome: OutcomePass, LastReason: ReasonNone},
	} {
		require.False(t, quotaBlocked(st))
	}
	require.True(t, quotaBlocked(AccountState{LastOutcome: OutcomeSkipped, LastReason: ReasonCoolingDown}))
}
