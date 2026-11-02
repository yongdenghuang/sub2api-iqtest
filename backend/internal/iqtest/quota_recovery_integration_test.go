//go:build integration

package iqtest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestQuotaRecoveryPersistenceAndRetryBackoff(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine", tcpostgres.WithDatabase("quota_recovery_test"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts (id BIGINT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', platform TEXT NOT NULL DEFAULT 'openai',
	 type TEXT NOT NULL DEFAULT 'oauth',status TEXT NOT NULL DEFAULT 'active',schedulable BOOLEAN NOT NULL DEFAULT true,
	 parent_account_id BIGINT,auto_pause_on_expired BOOLEAN NOT NULL DEFAULT true,expires_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ,rate_limit_reset_at TIMESTAMPTZ,extra JSONB NOT NULL DEFAULT '{}');
	 INSERT INTO accounts(id) VALUES(1),(2),(3),(4),(5);
	 UPDATE accounts SET rate_limit_reset_at=now()+interval '3 days' WHERE id=1;
	 UPDATE accounts SET schedulable=false WHERE id=3;
	 UPDATE accounts SET status='error' WHERE id=4;`)
	require.NoError(t, err)
	s := &Store{db: db}
	require.NoError(t, s.Migrate(ctx))
	p := newPlan()
	p.Enabled = false
	require.NoError(t, s.CreatePlan(ctx, p))
	for _, id := range []int64{1, 2, 3, 4, 5} {
		planID := int64(0)
		if id == 5 {
			planID = p.ID
		}
		require.NoError(t, s.UpsertState(ctx, &StateRow{AccountID: id, Kind: TestCandy, PlanID: planID,
			AccountState: AccountState{LastOutcome: OutcomeInfraError, LastReason: ReasonQuotaExhausted, UpdatedAt: time.Now()}}))
	}
	ids, err := s.quotaRecoveryCandidates(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, ids)
	checks, err := s.quotaBlockedChecks(ctx, 1)
	require.NoError(t, err)
	require.Len(t, checks, 1)
	checks, err = s.quotaBlockedChecks(ctx, 5)
	require.NoError(t, err)
	require.Empty(t, checks, "disabled plans must not retry")
	runID, err := s.CreateRun(ctx, 0, "quota_recovery", 1)
	require.NoError(t, err)
	require.NoError(t, s.InsertResult(ctx, &ResultRecord{RunID: runID, AccountID: 1, Result: Result{Kind: TestCandy, Outcome: OutcomeInfraError, Reason: ReasonQuotaExhausted}}))
	checks, err = s.quotaBlockedChecks(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, checks, "avoid repeated paid tests within five minutes")
	_, err = db.Exec(`UPDATE iq_results SET created_at=now()-interval '6 minutes' WHERE account_id=1`)
	require.NoError(t, err)
	checks, err = s.quotaBlockedChecks(ctx, 1)
	require.NoError(t, err)
	require.Len(t, checks, 1)
	// Once a real test passed, historical quota failures must not trigger probes.
	require.NoError(t, s.UpsertState(ctx, &StateRow{AccountID: 2, Kind: TestCandy, AccountState: AccountState{LastOutcome: OutcomePass, UpdatedAt: time.Now()}}))
	ids, err = s.quotaRecoveryCandidates(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, ids)
}
