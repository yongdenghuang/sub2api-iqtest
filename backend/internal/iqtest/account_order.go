package iqtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

const maxOrderedAccounts = 10000
const accountOrderLockKey = 7392012

var errAccountOrderConflict = errors.New("account list or order changed; reload before saving")
var errInvalidAccountOrder = errors.New("order must contain every current account exactly once")

type orderedAccount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type accountOrder struct {
	Accounts []orderedAccount `json:"accounts"`
	Revision string           `json:"revision"`
}

type accountOrderQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadAccountOrder(ctx context.Context, q accountOrderQuery, lock bool) (*accountOrder, error) {
	query := `SELECT a.id,a.name,a.platform FROM accounts a
		LEFT JOIN iq_account_display_order o ON o.account_id=a.id
		WHERE a.deleted_at IS NULL ORDER BY o.position ASC NULLS LAST,a.name ASC,a.id ASC LIMIT 10001`
	if lock {
		query += ` FOR SHARE OF a`
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := &accountOrder{Accounts: []orderedAccount{}}
	var ids strings.Builder
	for rows.Next() {
		var a orderedAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.Platform); err != nil {
			return nil, err
		}
		result.Accounts = append(result.Accounts, a)
		ids.WriteString(strconv.FormatInt(a.ID, 10))
		ids.WriteByte(',')
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result.Accounts) > maxOrderedAccounts {
		return nil, errors.New("manual ordering supports at most 10000 accounts")
	}
	result.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(ids.String())))
	return result, nil
}

func validateAccountOrder(ids []int64) error {
	if len(ids) == 0 || len(ids) > maxOrderedAccounts {
		return errInvalidAccountOrder
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return errInvalidAccountOrder
		}
		seen[id] = true
	}
	return nil
}

func (s *Store) saveAccountOrder(ctx context.Context, ids []int64, revision string) error {
	if err := validateAccountOrder(ids); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, accountOrderLockKey); err != nil {
		return err
	}
	current, err := loadAccountOrder(ctx, tx, true)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return errAccountOrderConflict
	}
	if len(ids) != len(current.Accounts) {
		return errInvalidAccountOrder
	}
	valid := make(map[int64]bool, len(current.Accounts))
	for _, a := range current.Accounts {
		valid[a.ID] = true
	}
	for _, id := range ids {
		if !valid[id] {
			return errInvalidAccountOrder
		}
	}
	// A separate table keeps account credentials, priority and scheduling caches
	// untouched. Concurrent creates simply appear after these saved positions.
	if _, err := tx.ExecContext(ctx, `INSERT INTO iq_account_display_order (account_id,position)
		SELECT id,ordinal FROM unnest($1::bigint[]) WITH ORDINALITY AS ordered(id,ordinal)
		ON CONFLICT (account_id) DO UPDATE SET position=EXCLUDED.position`, pq.Array(ids)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) getAccountOrder(c *gin.Context) {
	result, err := loadAccountOrder(c.Request.Context(), s.store.db, false)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (s *Service) saveAccountOrder(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var body struct {
		IDs      []int64 `json:"account_ids"`
		Revision string  `json:"revision"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Revision) != 64 {
		response.BadRequest(c, "invalid account order")
		return
	}
	err := s.store.saveAccountOrder(c.Request.Context(), body.IDs, body.Revision)
	switch {
	case errors.Is(err, errAccountOrderConflict):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, errInvalidAccountOrder):
		response.BadRequest(c, err.Error())
	case err != nil:
		response.InternalError(c, err.Error())
	default:
		response.Success(c, gin.H{"saved": len(body.IDs)})
	}
}
