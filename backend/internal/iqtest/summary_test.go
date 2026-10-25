package iqtest

import (
	"database/sql"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
	"net/http/httptest"
	"testing"
)

func TestAccountSummaryCountsAllActiveRowsOnce(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE accounts (id INTEGER, deleted_at DATETIME);
 CREATE TABLE iq_account_state (account_id INTEGER, kind TEXT, state TEXT);
 INSERT INTO accounts VALUES (1,NULL),(2,NULL),(3,NULL),(4,NULL),(5,NULL),(6,'2026-01-01');
 INSERT INTO iq_account_state VALUES
 (1,'candy','{"last_outcome":"quality_fail"}'), (1,'pelican_svg','{"last_outcome":"infra_error"}'),
 (2,'candy','{"last_outcome":"pass"}'), (2,'pelican_svg','{"last_outcome":"infra_error"}'),
 (3,'candy','{"last_outcome":"pass"}'), (4,'candy','{"last_outcome":"skipped"}'),
 (6,'candy','{"last_outcome":"quality_fail"}');`)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/summary?page=2&iq_quality=normal", nil)
	(&Service{store: &Store{db: db}}).accountSummary(c)
	require.Equal(t, 200, w.Code)
	var body struct {
		Data map[string]int `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, map[string]int{"total": 5, "quality_fail": 1, "infra_error": 1, "normal": 1, "untested": 2}, body.Data)
}
