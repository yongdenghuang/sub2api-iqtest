// Package carpool synchronizes opt-in subscription groups with an upstream
// weekly quota. It never modifies a user's wallet or consumes reset credits.
package carpool

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const week = 7 * 24 * time.Hour

type Observation struct {
	At      time.Time `json:"at"`
	Percent float64   `json:"percent"`
	Cost    float64   `json:"cost"`
}

type State struct {
	ResetAt      time.Time   `json:"reset_at"`
	CheckedAt    time.Time   `json:"checked_at"`
	Percent      float64     `json:"percent"`
	Capacity     float64     `json:"capacity"`
	Estimated    bool        `json:"estimated"`
	Baseline     Observation `json:"baseline"`
	Candidate    time.Time   `json:"candidate"`
	CandidateAt  time.Time   `json:"candidate_at"`
	DropAt       time.Time   `json:"drop_at"`
	Message      string      `json:"message"`
	LastError    string      `json:"last_error"`
	Members      int         `json:"members"`
	PerMemberUSD float64     `json:"per_member_usd"`
}

func weeklyObservation(usage *service.OpenAIQuotaUsage, now time.Time) (float64, time.Time, error) {
	if usage == nil || usage.RateLimit == nil {
		return 0, time.Time{}, errors.New("官方额度响应缺少周窗口")
	}
	for _, w := range []*service.OpenAIRateLimitWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if w == nil || w.LimitWindowSeconds != int64(week/time.Second) {
			continue
		}
		reset := time.Unix(w.ResetAt, 0).UTC()
		if !finite(w.UsedPercent) || w.UsedPercent < 0 || w.UsedPercent > 100 || !reset.After(now) || reset.After(now.Add(week+5*time.Minute)) {
			return 0, time.Time{}, errors.New("官方周额度数据无效或已经过期")
		}
		return w.UsedPercent, reset, nil
	}
	return 0, time.Time{}, errors.New("没有找到官方 7 天额度窗口")
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// observeWindow requires two independent, consistent observations of a reset.
// A quota percentage drop alone may be a capacity increase, not a reset.
func observeWindow(st *State, percent float64, reset, now time.Time) (changed, ready bool) {
	st.CheckedAt, st.LastError = now, ""
	if st.ResetAt.IsZero() {
		st.ResetAt, st.Percent = reset, percent
		st.Message = "采样中，保留初始均分额度"
		return false, true
	}
	delta := reset.Sub(st.ResetAt)
	if delta > 5*time.Minute {
		natural := !now.Before(st.ResetAt) && delta >= 24*time.Hour
		early := percent+5 <= st.Percent
		if !natural && !early {
			st.Message = "官方重置时间变化，等待用量确认"
			return false, false
		}
		if st.Candidate.IsZero() || absDuration(reset.Sub(st.Candidate)) > 5*time.Minute {
			st.Candidate, st.CandidateAt = reset, now
			st.Message = "检测到官方重置，等待第二次确认"
			return false, false
		}
		if now.Sub(st.CandidateAt) < 45*time.Second {
			return false, false
		}
		st.ResetAt = st.Candidate
		st.Candidate, st.CandidateAt = time.Time{}, time.Time{}
		st.Baseline, st.Estimated = Observation{}, false
		st.Percent = percent
		st.Message = "已同步官方重置，重新采样"
		return true, true
	}
	st.Candidate, st.CandidateAt = time.Time{}, time.Time{}
	if delta < -5*time.Minute {
		st.Message = "官方读数回退但重置未确认，暂不调整额度"
		return false, false
	}
	if percent+1 < st.Percent {
		if st.DropAt.IsZero() {
			st.DropAt = now
			st.Message = "官方用量比例下降，等待确认额度变化"
			return false, false
		}
		if now.Sub(st.DropAt) < 45*time.Second {
			return false, false
		}
		// A larger capacity can reduce the percentage in the same window.
		// Relearn its cost ratio without clearing any subscriber's usage.
		st.Baseline = Observation{}
		st.Message = "官方额度比例变化，保留用量并重新采样"
	}
	st.DropAt = time.Time{}
	st.Percent = percent
	return false, true
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// estimate uses only cost accrued between our own fresh observations. It does
// not assume retained request logs contain the entire historical week. A one
// percentage-point margin covers integer rounding at the endpoints.
func estimate(st *State, obs Observation) {
	if st.Baseline.At.IsZero() {
		st.Baseline = obs
		return
	}
	span, cost := obs.Percent-st.Baseline.Percent, obs.Cost-st.Baseline.Cost
	if cost < -0.0001 {
		st.Baseline, st.Estimated = obs, false
		st.Message = "用量记录发生变化，重新采样"
		return
	}
	if span < 5 || cost < 1 || obs.At.Sub(st.Baseline.At) < 2*time.Minute {
		return
	}
	capacity := cost * 100 / (span + 1)
	if !finite(capacity) || capacity < 1 || capacity > 1000000 {
		return
	}
	if st.Estimated {
		capacity = (st.Capacity + capacity) / 2
	}
	st.Capacity, st.Estimated, st.Baseline = capacity, true, obs
	st.Message = "按官方用量测算并均分"
}

func fairLimit(st State, usage []float64, excludedUsage float64) float64 {
	if len(usage) == 0 {
		return 0
	}
	var spent float64
	for _, v := range usage {
		spent += v
	}
	remaining := math.Max(0, st.Capacity-excludedUsage-spent)
	if st.Estimated {
		remaining = st.Capacity * math.Max(0, 1-st.Percent/100)
	}
	// Find the common cap such that SUM(max(cap - alreadySpent, 0)) fits
	// remaining capacity. A member who already exceeded a reduced share cannot
	// "return" spent quota to finance other members (which averaging would do).
	ordered := append([]float64(nil), usage...)
	sort.Float64s(ordered)
	level := ordered[0]
	for i := 1; i < len(ordered); i++ {
		step := (ordered[i] - level) * float64(i)
		if remaining <= step {
			level += remaining / float64(i)
			return math.Max(0.00000001, math.Floor((level+1e-9)*100)/100)
		}
		remaining -= step
		level = ordered[i]
	}
	level += remaining / float64(len(ordered))
	return math.Max(0.00000001, math.Floor((level+1e-9)*100)/100)
}
