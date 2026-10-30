//go:build integration

package iqtest

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestVisualReviewMigrationRetiresLegacyPasses(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine", tcpostgres.WithDatabase("visual_test"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY); INSERT INTO accounts VALUES(1);
 CREATE TABLE iq_schema_migrations(version INT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT now()); INSERT INTO iq_schema_migrations(version) VALUES(1),(2);`)
	require.NoError(t, err)
	_, err = db.Exec(migrations[0])
	require.NoError(t, err)
	_, err = db.Exec(migrations[1])
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO iq_runs(id,trigger,status,accounts,requests,pass) VALUES(1,'adhoc','done',1,2,2);
 INSERT INTO iq_results(id,run_id,account_id,kind,outcome,output,score,metrics) VALUES
 (1,1,1,'pelican_svg','pass','original svg',100,'{"shapes":20}'),(2,1,1,'candy','pass','21',0,NULL);
 INSERT INTO iq_account_state(account_id,kind,state) VALUES
 (1,'pelican_svg','{"state":"normal","last_outcome":"pass","recent":[true,true],"consecutive_passes":2}'),
 (1,'candy','{"state":"normal","last_outcome":"pass","recent":[true]}');`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO iq_plans(name,config) VALUES ('old default','{"checks":[{"kind":"candy","model":"gpt-6-astra"},{"kind":"pelican_svg","model":"custom"}]}');`)
	require.NoError(t, err)
	store := &Store{db: db}
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))
	var auto, custom string
	require.NoError(t, db.QueryRow(`SELECT config->'checks'->0->>'model',config->'checks'->1->>'model' FROM iq_plans WHERE name='old default'`).Scan(&auto, &custom))
	require.Equal(t, AutoModel, auto)
	require.Equal(t, "custom", custom)
	_, err = db.Exec(`SELECT setval(pg_get_serial_sequence('iq_results','id'),100)`)
	require.NoError(t, err)
	fresh := &ResultRecord{RunID: 1, AccountID: 1, Result: Result{Kind: TestCandy, Outcome: OutcomePass, Model: "gpt-6-luna"}}
	require.NoError(t, store.InsertResult(ctx, fresh))
	stored, err := store.GetResult(ctx, fresh.ID)
	require.NoError(t, err)
	require.Equal(t, "gpt-6-luna", stored.Model)
	result, err := store.GetResult(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, OutcomeSkipped, result.Outcome)
	require.Equal(t, ReasonVisualReviewRequired, result.Reason)
	require.Zero(t, result.Score)
	require.Equal(t, "original svg", result.Output)
	var oldScore int
	require.NoError(t, db.QueryRow(`SELECT (metrics->>'legacy_structure_score')::int FROM iq_results WHERE id=1`).Scan(&oldScore))
	require.Equal(t, 100, oldScore)
	run, err := store.GetRun(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, run.Pass)
	require.Equal(t, 1, run.Skipped)
	require.Equal(t, 2, run.Requests)
	state, _, err := store.GetState(ctx, 1, TestPelicanSVG)
	require.NoError(t, err)
	require.Empty(t, state.Recent)
	require.Equal(t, OutcomeSkipped, state.LastOutcome)
	candy, _, err := store.GetState(ctx, 1, TestCandy)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, candy.Recent)
}
