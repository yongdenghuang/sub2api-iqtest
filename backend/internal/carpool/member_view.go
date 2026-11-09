package carpool

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// MemberView is deliberately separate from the administrator's Group/State.
// It contains no upstream identity, email, credentials, wallet, or raw errors.
type MemberView struct {
	Number       int     `json:"number"`
	Name         string  `json:"name"`
	IsSelf       bool    `json:"is_self"`
	AllocatedUSD float64 `json:"allocated_usd"`
	UsedUSD      float64 `json:"used_usd"`
	RemainingUSD float64 `json:"remaining_usd"`
}

type MemberGroupView struct {
	ID                  int64               `json:"id"`
	Name                string              `json:"name"`
	MemberCount         int                 `json:"member_count"`
	AllocationReady     bool                `json:"allocation_ready"`
	AllocationMode      string              `json:"allocation_mode"`
	SyncOfficialReset   bool                `json:"sync_official_reset"`
	TotalUSD            float64             `json:"total_usd"`
	UsedUSD             float64             `json:"used_usd"`
	RemainingUSD        float64             `json:"remaining_usd"`
	PerMemberUSD        float64             `json:"per_member_usd"`
	Estimated           bool                `json:"estimated"`
	UpstreamCapacityUSD *float64            `json:"upstream_capacity_usd"`
	OfficialUsedPercent *float64            `json:"official_used_percent"`
	NextResetAt         *time.Time          `json:"next_reset_at"`
	LastSyncAt          *time.Time          `json:"last_sync_at"`
	Status              string              `json:"status"`
	Members             []MemberView        `json:"members"`
	Windows             []MemberUsageWindow `json:"windows"`
}

// ListForMember is a read-only snapshot. Authorization and the member roster
// use the same transaction, so concurrent membership edits cannot mix views.
// Reading this endpoint never queries upstream or changes billing counters.
func (s *Service) ListForMember(ctx context.Context, userID int64) ([]MemberGroupView, error) {
	result := []MemberGroupView{}
	if userID <= 0 {
		return result, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT g.id,g.name,COALESCE(g.weekly_limit_usd,0),cg.state,cg.allocation_mode,cg.sync_official_reset
 FROM carpool_groups cg JOIN groups g ON g.id=cg.group_id
 WHERE cg.enabled AND g.deleted_at IS NULL AND g.status='active'
 AND g.platform='openai' AND g.subscription_type='subscription'
 AND EXISTS (SELECT 1 FROM user_subscriptions s JOIN users u ON u.id=s.user_id
 WHERE s.group_id=g.id AND s.user_id=$1 AND s.deleted_at IS NULL AND s.status='active'
 AND s.starts_at<=now() AND s.expires_at>now() AND u.deleted_at IS NULL AND u.status='active')
 ORDER BY g.id`, userID)
	if err != nil {
		return nil, err
	}
	var states []State
	for rows.Next() {
		var g MemberGroupView
		var raw []byte
		if err = rows.Scan(&g.ID, &g.Name, &g.PerMemberUSD, &raw, &g.AllocationMode, &g.SyncOfficialReset); err != nil {
			break
		}
		var st State
		if err = json.Unmarshal(raw, &st); err != nil {
			break
		}
		result = append(result, g)
		states = append(states, st)
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if rowErr != nil {
		return nil, rowErr
	}
	now := time.Now().UTC()
	for i := range result {
		g := &result[i]
		g.Members, err = readMemberRoster(ctx, tx, g.ID, userID, g.PerMemberUSD)
		if err != nil {
			return nil, err
		}
		completeMemberView(g, states[i], now)
		g.Windows, err = readMemberWindows(ctx, tx, g.ID, userID, states[i], now)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func readMemberRoster(ctx context.Context, q querier, groupID, viewerID int64, limit float64) ([]MemberView, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.user_id,COALESCE(u.username,''),s.weekly_usage_usd
 FROM user_subscriptions s JOIN users u ON u.id=s.user_id
 WHERE s.group_id=$1 AND s.deleted_at IS NULL AND s.status='active'
 AND s.starts_at<=now() AND s.expires_at>now() AND u.deleted_at IS NULL AND u.status='active'
 ORDER BY s.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	members := []MemberView{}
	for rows.Next() {
		var id int64
		var m MemberView
		if err = rows.Scan(&id, &m.Name, &m.UsedUSD); err != nil {
			return nil, err
		}
		m.Name = strings.TrimSpace(m.Name)
		// Some imports put an email in username. Use a local seat label instead.
		if strings.Contains(m.Name, "@") {
			m.Name = ""
		}
		m.Number, m.IsSelf = len(members)+1, id == viewerID
		m.AllocatedUSD = math.Max(0, limit)
		m.RemainingUSD = math.Max(0, m.AllocatedUSD-m.UsedUSD)
		members = append(members, m)
	}
	return members, rows.Err()
}

func completeMemberView(g *MemberGroupView, st State, now time.Time) {
	g.MemberCount = len(g.Members)
	g.AllocationReady = g.PerMemberUSD > 0
	g.TotalUSD = math.Max(0, g.PerMemberUSD) * float64(g.MemberCount)
	for _, m := range g.Members {
		g.UsedUSD += m.UsedUSD
		// A reduced cap may leave one member over budget. Their spent quota
		// must not remove another member's remaining allowance from the display.
		g.RemainingUSD += m.RemainingUSD
	}
	// Aggregate totals remain shared, but another member's identity and usage
	// must never leave the server. Keep the array for older clients as well.
	mine := []MemberView{}
	for _, m := range g.Members {
		if m.IsSelf {
			mine = append(mine, m)
		}
	}
	g.Members = mine
	automatic := g.AllocationMode != AllocationManual
	g.Estimated = automatic && st.Estimated
	if g.Estimated && finite(st.Capacity) && st.Capacity > 0 {
		g.UpstreamCapacityUSD = &st.Capacity
	}
	if !st.ResetAt.IsZero() {
		g.NextResetAt = &st.ResetAt
	}
	if !st.CheckedAt.IsZero() {
		g.LastSyncAt = &st.CheckedAt
		if finite(st.Percent) && st.Percent >= 0 && st.Percent <= 100 {
			g.OfficialUsedPercent = &st.Percent
		}
	}
	switch {
	case st.LastError != "":
		g.Status = "delayed"
	case st.CheckedAt.IsZero():
		g.Status = "waiting"
	case now.Sub(st.CheckedAt) > 3*time.Minute:
		g.Status = "delayed"
	case g.SyncOfficialReset && (!st.Candidate.IsZero() || (!st.ResetAt.IsZero() && !st.ResetAt.After(now))):
		g.Status = "pending_reset"
	case automatic && (!st.DropAt.IsZero() || st.Members != g.MemberCount):
		g.Status = "adjusting"
	case automatic && !st.Estimated:
		g.Status = "sampling"
	default:
		g.Status = "synced"
	}
}
