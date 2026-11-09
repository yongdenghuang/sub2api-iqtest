package carpool

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// OfficialWindow is a display snapshot, independent of the two-observation
// confirmation used to mutate subscription counters. No credentials are kept.
type OfficialWindow struct {
	Window      string    `json:"window"`
	UsedPercent float64   `json:"used_percent"`
	ResetAt     time.Time `json:"reset_at"`
}

type MemberWindowStats struct {
	Requests       int64   `json:"requests"`
	Tokens         int64   `json:"tokens"`
	AccountCostUSD float64 `json:"account_cost_usd"`
	UserCostUSD    float64 `json:"user_cost_usd"`
}

type MemberUsageWindow struct {
	Window              string             `json:"window"`
	OfficialUsedPercent *float64           `json:"official_used_percent"`
	StartAt             *time.Time         `json:"start_at"`
	ResetAt             *time.Time         `json:"reset_at"`
	Stats               *MemberWindowStats `json:"stats"`
}

func windowDuration(name string) time.Duration {
	if name == "5h" {
		return 5 * time.Hour
	}
	if name == "7d" {
		return week
	}
	return 0
}

// Match explicit durations, not primary/secondary slot positions or Spark
// limits. A recently ended window is retained until a new official window is
// observed; the UI labels it as awaiting an update instead of claiming a reset.
func officialWindows(usage *service.OpenAIQuotaUsage, now time.Time) []OfficialWindow {
	windows := []OfficialWindow{}
	if usage == nil || usage.RateLimit == nil {
		return windows
	}
	for _, name := range []string{"5h", "7d"} {
		duration := windowDuration(name)
		for _, w := range []*service.OpenAIRateLimitWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
			if w == nil || w.LimitWindowSeconds != int64(duration/time.Second) || !finite(w.UsedPercent) || w.UsedPercent < 0 || w.UsedPercent > 100 {
				continue
			}
			reset := time.Unix(w.ResetAt, 0).UTC()
			if w.ResetAt <= 0 || reset.After(now.Add(duration+5*time.Minute)) || reset.Before(now.Add(-duration)) {
				continue
			}
			windows = append(windows, OfficialWindow{Window: name, UsedPercent: w.UsedPercent, ResetAt: reset})
			break
		}
	}
	return windows
}

func readMemberWindows(ctx context.Context, q querier, groupID, userID int64, st State, now time.Time) ([]MemberUsageWindow, error) {
	snapshots := st.Windows
	// Upgrade compatibility: older snapshots had only the confirmed weekly
	// window. Never fabricate a five-hour window from the weekly percentage.
	if len(snapshots) == 0 && !st.CheckedAt.IsZero() && !st.ResetAt.IsZero() {
		snapshots = []OfficialWindow{{Window: "7d", UsedPercent: st.Percent, ResetAt: st.ResetAt}}
	}
	windows := make([]MemberUsageWindow, 0, 2)
	for _, name := range []string{"5h", "7d"} {
		w := MemberUsageWindow{Window: name}
		for _, snapshot := range snapshots {
			if snapshot.Window != name || snapshot.ResetAt.IsZero() || !finite(snapshot.UsedPercent) || snapshot.UsedPercent < 0 || snapshot.UsedPercent > 100 {
				continue
			}
			start, reset := snapshot.ResetAt.Add(-windowDuration(name)), snapshot.ResetAt
			if start.After(now) {
				continue
			}
			w.StartAt, w.ResetAt, w.OfficialUsedPercent = &start, &reset, &snapshot.UsedPercent
			end := now
			if reset.Before(end) {
				end = reset
			}
			var err error
			w.Stats, err = readMemberWindowStats(ctx, q, groupID, userID, start, end)
			if err != nil {
				return nil, err
			}
			break
		}
		windows = append(windows, w)
	}
	return windows, nil
}

func readMemberWindowStats(ctx context.Context, q querier, groupID, userID int64, start, end time.Time) (*MemberWindowStats, error) {
	stats := &MemberWindowStats{}
	// Both identity and car must match. Subscription/account checks also exclude
	// monitor probes, another car using the same source, and historical bindings.
	// Token and A/U cost formulas match the native account window statistics.
	err := q.QueryRowContext(ctx, `SELECT COUNT(*),
 COALESCE(SUM(l.input_tokens+l.output_tokens+l.cache_creation_tokens+l.cache_read_tokens),0),
 COALESCE(SUM(COALESCE(l.account_stats_cost,l.total_cost)*COALESCE(l.account_rate_multiplier,1)),0),
 COALESCE(SUM(l.actual_cost),0)
 FROM usage_logs l JOIN user_subscriptions s ON s.id=l.subscription_id
 JOIN carpool_groups cg ON cg.group_id=s.group_id
 WHERE l.user_id=$1 AND s.user_id=$1 AND l.group_id=$2 AND s.group_id=$2
 AND l.account_id=cg.account_id AND s.deleted_at IS NULL AND s.status='active'
 AND s.starts_at<=now() AND s.expires_at>now() AND l.created_at>=s.starts_at
 AND l.created_at >= $3 AND l.created_at < $4`, userID, groupID, start, end).
		Scan(&stats.Requests, &stats.Tokens, &stats.AccountCostUSD, &stats.UserCostUSD)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
