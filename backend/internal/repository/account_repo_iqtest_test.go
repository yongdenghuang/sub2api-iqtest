package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAccountIQAndManualSortAcrossPages(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT, deleted_at DATETIME);
 CREATE TABLE iq_account_state (account_id INTEGER, kind TEXT, state TEXT);
 CREATE TABLE iq_account_display_order (account_id INTEGER PRIMARY KEY, position INTEGER);
 INSERT INTO accounts(id,name) VALUES (1,'b'),(2,'d'),(3,'c'),(4,'a'),(5,'e'),(6,'f'),(7,'g');
 UPDATE accounts SET deleted_at=CURRENT_TIMESTAMP WHERE id=7;
 INSERT INTO iq_account_state VALUES
 (1,'candy','{"last_outcome":"pass"}'),(1,'pelican_svg','{"last_outcome":"quality_fail"}'),
 (2,'candy','{"last_outcome":"quality_fail"}'),(2,'pelican_svg','{"last_outcome":"pass"}'),
 (3,'candy','{"last_outcome":"infra_error"}'),(4,'candy','{"last_outcome":"skipped"}'),
 (5,'pelican_svg','{"last_outcome":"quality_fail"}'),(7,'candy','{"last_outcome":"quality_fail"}');
 INSERT INTO iq_account_display_order VALUES (2,1),(1,2),(5,3);`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	for _, tc := range []struct {
		key, direction string
		want           []int64
	}{
		{"iq_status", "asc", []int64{2, 1, 3, 4, 5, 6}},
		{"iq_status", "desc", []int64{1, 2, 3, 4, 5, 6}},
		{"display_order", "asc", []int64{2, 1, 5, 4, 3, 6}},
	} {
		t.Run(tc.key+tc.direction, func(t *testing.T) {
			q := client.Account.Query().Where(dbaccount.DeletedAtIsNil())
			for _, order := range accountListOrder(pagination.PaginationParams{SortBy: tc.key, SortOrder: tc.direction}) {
				q = q.Order(order)
			}
			var got []int64
			for page := 0; page < 3; page++ {
				ids, err := q.Clone().Limit(2).Offset(page * 2).IDs(context.Background())
				require.NoError(t, err)
				got = append(got, ids...)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestIQQualityFilterBeforePagination(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, deleted_at DATETIME);
 CREATE TABLE iq_account_state (account_id INTEGER, kind TEXT, state TEXT);
 INSERT INTO accounts(id) VALUES (1),(2),(3),(4),(5),(6),(7),(8);
 UPDATE accounts SET deleted_at = CURRENT_TIMESTAMP WHERE id = 8;`)
	require.NoError(t, err)
	fixtures := []struct {
		id            int
		kind, outcome string
	}{
		{2, "candy", "pass"}, {3, "candy", "quality_fail"}, {3, "pelican_svg", "infra_error"},
		{4, "candy", "infra_error"}, {4, "pelican_svg", "pass"}, {5, "candy", "skipped"},
		{6, "pelican_svg", "quality_fail"}, {7, "candy", "pass"}, {7, "pelican_svg", "skipped"},
		{8, "candy", "quality_fail"},
	}
	for _, f := range fixtures {
		_, err = db.Exec(`INSERT INTO iq_account_state VALUES (?, ?, ?)`, f.id, f.kind, fmt.Sprintf(`{"last_outcome":%q}`, f.outcome))
		require.NoError(t, err)
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	for _, tc := range []struct {
		filter string
		ids    []int64
	}{
		{"", []int64{1, 2, 3, 4, 5, 6, 7}}, {"quality_fail", []int64{3, 6}},
		{"infra_error", []int64{4}}, {"normal", []int64{2, 7}}, {"untested", []int64{1, 5}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			ctx, err := iqquality.WithFilter(context.Background(), tc.filter)
			require.NoError(t, err)
			q := applyIQQualityFilter(ctx, client.Account.Query().Where(dbaccount.DeletedAtIsNil()))
			total, err := q.Clone().Count(ctx)
			require.NoError(t, err)
			require.Equal(t, len(tc.ids), total)
			var got []int64
			for page := 0; page < total; page++ {
				ids, err := q.Clone().Order(dbent.Asc("id")).Limit(1).Offset(page).IDs(ctx)
				require.NoError(t, err)
				got = append(got, ids...)
			}
			require.Equal(t, tc.ids, got)
		})
	}
	_, err = iqquality.WithFilter(context.Background(), "normal' OR 1=1")
	require.Error(t, err)
}

// PostgreSQL requires numbered placeholders even when earlier account filters
// have already bound parameters. SQLite accepts '?' and cannot catch this alone.
func TestIQQualityFilterPostgresParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	ctx, err := iqquality.WithFilter(context.Background(), "quality_fail")
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT COUNT.*CASE.* = \$2`).WithArgs("openai", "quality_fail").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	q := client.Account.Query().Where(dbaccount.PlatformEQ("openai"), dbaccount.DeletedAtIsNil())
	count, err := applyIQQualityFilter(ctx, q).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, mock.ExpectationsWereMet())
}
