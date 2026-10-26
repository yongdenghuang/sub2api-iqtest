//go:build integration

package iqtest

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAccountOrderPostgresPersistence(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine", tcpostgres.WithDatabase("account_order_test"),
		tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts (id BIGINT PRIMARY KEY, name TEXT NOT NULL, platform TEXT NOT NULL,
		deleted_at TIMESTAMPTZ, priority INTEGER NOT NULL DEFAULT 50, credentials JSONB NOT NULL DEFAULT '{"token":"unchanged"}');
		INSERT INTO accounts(id,name,platform) VALUES (1,'A','openai'),(2,'B','openai'),(3,'C','openai');`)
	require.NoError(t, err)
	store := &Store{db: db}
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx), "migration must be idempotent")
	initial, err := loadAccountOrder(ctx, db, false)
	require.NoError(t, err)
	require.NoError(t, store.saveAccountOrder(ctx, []int64{3, 1, 2}, initial.Revision))
	saved, err := loadAccountOrder(ctx, db, false)
	require.NoError(t, err)
	ids := func(order *accountOrder) []int64 {
		result := make([]int64, 0, len(order.Accounts))
		for _, a := range order.Accounts {
			result = append(result, a.ID)
		}
		return result
	}
	require.Equal(t, []int64{3, 1, 2}, ids(saved))
	require.ErrorIs(t, store.saveAccountOrder(ctx, []int64{2, 1, 3}, initial.Revision), errAccountOrderConflict)

	// Only one concurrent editor may replace the same saved revision.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, order := range [][]int64{{1, 2, 3}, {2, 3, 1}} {
		wg.Add(1)
		go func(order []int64) {
			defer wg.Done()
			results <- store.saveAccountOrder(ctx, order, saved.Revision)
		}(order)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, errAccountOrderConflict)
		}
	}
	require.Equal(t, 1, successes)
	var unchanged int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM accounts WHERE priority=50 AND credentials->>'token'='unchanged'`).Scan(&unchanged))
	require.Equal(t, 3, unchanged)
	beforeCreate, err := loadAccountOrder(ctx, db, false)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts(id,name,platform) VALUES (4,'0-new','openai')`)
	require.NoError(t, err)
	withNew, err := loadAccountOrder(ctx, db, false)
	require.NoError(t, err)
	require.Equal(t, int64(4), withNew.Accounts[3].ID, "new accounts follow saved positions")
	require.ErrorIs(t, store.saveAccountOrder(ctx, ids(beforeCreate), beforeCreate.Revision), errAccountOrderConflict)
}
