package carpool

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

const directPreviewGroup = "四人车ujjw"

type DirectPreview struct {
	AccountID int64     `json:"account_id"`
	From      time.Time `json:"from"`
	AsOf      time.Time `json:"as_of"`
	ResetAt   time.Time `json:"reset_at"`
	Percent   float64   `json:"percent"`
	Capacity  float64   `json:"capacity"`
	Official  float64   `json:"official"`
	Site      float64   `json:"site"`
	Remaining float64   `json:"remaining"`
	Direct    *float64  `json:"direct"`
	Suggested *float64  `json:"suggested"`
}

// directPreview reads the latest confirmed weekly snapshot and the matching
// billed traffic through a read-only transaction. The calibration is supplied
// by the administrator and is independent of the automatic allocator.
func (s *Service) directPreview(ctx context.Context, id int64, capacity float64) (DirectPreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return DirectPreview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	g, err := loadGroup(ctx, tx, id)
	if err != nil {
		return DirectPreview{}, err
	}
	if g.Name != directPreviewGroup || !g.Eligible || g.AccountCount != 1 || g.AccountID <= 0 || g.BindingChanged {
		return DirectPreview{}, errDirectPreviewBinding
	}
	if len(g.Members) != 3 {
		return DirectPreview{}, errDirectPreviewMembers
	}

	p, err := directPreviewWindow(g, capacity, time.Now().UTC())
	if err != nil {
		return DirectPreview{}, err
	}
	// Use standard cost for the account total and for each participant. This
	// avoids mixing individual billing multipliers into the shared estimate.
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN group_id=$2 THEN COALESCE(subscription_id,0) ELSE 0 END,
 COALESCE(SUM(total_cost),0) FROM usage_logs
 WHERE account_id=$1 AND created_at >= $3 AND created_at < $4 GROUP BY 1`,
		g.AccountID, g.ID, p.From, p.AsOf)
	if err != nil {
		return DirectPreview{}, err
	}
	defer func() { _ = rows.Close() }()
	costs := make(map[int64]float64)
	for rows.Next() {
		var subscriptionID int64
		var cost float64
		if err = rows.Scan(&subscriptionID, &cost); err != nil {
			return DirectPreview{}, err
		}
		costs[subscriptionID] = cost
		p.Site += cost
	}
	if err = rows.Err(); err != nil {
		return DirectPreview{}, err
	}
	used := make([]float64, 0, 3)
	for _, m := range g.Members {
		used = append(used, costs[m.ID])
	}
	p.calculate(used)
	return p, nil
}

var (
	errDirectPreviewBinding = errors.New("试算仅适用于绑定已确认、只有一个 OpenAI OAuth 上游的四人车ujjw")
	errDirectPreviewMembers = errors.New("试算需要 3 位生效车友，直登车主作为第 4 人")
	errDirectPreviewWindow  = errors.New("暂无近期已确认的官方周用量，请等待额度同步后再试")
)

func directPreviewWindow(g Group, capacity float64, now time.Time) (DirectPreview, error) {
	st := g.State
	start := st.ResetAt.Add(-week)
	if st.CheckedAt.After(now) || now.Sub(st.CheckedAt) > 5*time.Minute ||
		!st.ResetAt.After(now) || !st.CheckedAt.After(start) ||
		st.LastError != "" || !st.Candidate.IsZero() || !st.DropAt.IsZero() ||
		!finite(st.Percent) || st.Percent < 0 || st.Percent > 100 {
		return DirectPreview{}, errDirectPreviewWindow
	}
	// Sync records the raw reading even when it is waiting to confirm a
	// changed reset boundary. Never use that reading with the old period.
	for _, w := range st.Windows {
		if w.Window == "7d" && absDuration(w.ResetAt.Sub(st.ResetAt)) <= 5*time.Minute && w.UsedPercent == st.Percent {
			return DirectPreview{
				AccountID: g.AccountID, From: start, AsOf: st.CheckedAt, ResetAt: st.ResetAt,
				Percent: st.Percent, Capacity: capacity,
			}, nil
		}
	}
	return DirectPreview{}, errDirectPreviewWindow
}

func (p *DirectPreview) calculate(memberUsage []float64) {
	p.Official = p.Capacity * p.Percent / 100
	p.Remaining = math.Max(0, p.Capacity-p.Official)
	if !finite(p.Site) || p.Site < 0 || p.Site > p.Official+0.000001 {
		return
	}
	direct := math.Max(0, p.Official-p.Site)
	used := append(append(make([]float64, 0, len(memberUsage)+1), memberUsage...), direct)
	limit := fairLimit(State{Capacity: p.Capacity, Percent: p.Percent, Estimated: true}, used, 0)
	p.Direct, p.Suggested = &direct, &limit
}
