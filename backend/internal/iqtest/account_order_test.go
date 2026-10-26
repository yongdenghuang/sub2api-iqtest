package iqtest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSaveAccountOrderRejectsInvalidIDsBeforeTransaction(t *testing.T) {
	for _, ids := range [][]int64{nil, {}, {0}, {-1}, {1, 1}, make([]int64, maxOrderedAccounts+1)} {
		require.ErrorIs(t, (&Store{}).saveAccountOrder(context.Background(), ids, ""), errInvalidAccountOrder)
	}
}

func TestSaveAccountOrderAtomicAndConcurrentChanges(t *testing.T) {
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte("1,2,")))
	for _, scenario := range []string{"save", "stale", "missing", "unknown", "write-error"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(accountOrderLockKey).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT a.id,a.name,a.platform.*FOR SHARE OF a").WillReturnRows(
				sqlmock.NewRows([]string{"id", "name", "platform"}).AddRow(1, "A", "openai").AddRow(2, "B", "openai"))
			ids, expected := []int64{2, 1}, revision
			switch scenario {
			case "save":
				mock.ExpectExec("INSERT INTO iq_account_display_order.*ON CONFLICT").WithArgs("{2,1}").WillReturnResult(sqlmock.NewResult(0, 2))
				mock.ExpectCommit()
			case "write-error":
				mock.ExpectExec("INSERT INTO iq_account_display_order").WillReturnError(errors.New("write failed"))
				mock.ExpectRollback()
			default:
				switch scenario {
				case "stale":
					expected = "stale"
				case "missing":
					ids = []int64{1}
				default:
					ids = []int64{2, 3}
				}
				mock.ExpectRollback()
			}
			err = (&Store{db: db}).saveAccountOrder(context.Background(), ids, expected)
			switch scenario {
			case "save":
				require.NoError(t, err)
			case "stale":
				require.ErrorIs(t, err, errAccountOrderConflict)
			default:
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountOrderRoutesRequireAdminMiddleware(t *testing.T) {
	router := gin.New()
	(&Service{}).RegisterRoutes(router.Group("/api/v1"), func(c *gin.Context) { c.AbortWithStatus(401) })
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/admin/iqtest/account-order", strings.NewReader(`{}`)))
		require.Equal(t, 401, w.Code)
	}
}
