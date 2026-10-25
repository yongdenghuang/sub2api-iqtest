package iqtest

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestGetRunLiveProgress(t *testing.T) {
	for _, status := range []string{"running", "done"} {
		t.Run(status, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT .* FROM iq_runs WHERE id").WithArgs(int64(7)).WillReturnRows(
				sqlmock.NewRows(strings.Split(runCols, ", ")).AddRow(7, 0, "adhoc", status, time.Now(), nil, 3, 4, 2, 1, 1, 2))
			if status == "running" {
				mock.ExpectQuery("SELECT.*COUNT.*FROM iq_results WHERE run_id").WithArgs(int64(7)).WillReturnRows(
					sqlmock.NewRows([]string{"requests", "pass", "quality_fail", "infra_error", "skipped"}).AddRow(2, 1, 0, 1, 0))
			}
			run, err := (&Store{db: db}).GetRun(context.Background(), 7)
			require.NoError(t, err)
			require.Equal(t, 3, run.Accounts)
			if status == "running" {
				require.Equal(t, 2, run.Requests)
				require.Equal(t, 1, run.Pass)
			} else {
				require.Equal(t, 4, run.Requests)
				require.Equal(t, 2, run.Pass)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
