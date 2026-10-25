package iqtest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduleType selects how a plan repeats.
type ScheduleType string

const (
	ScheduleInterval ScheduleType = "interval"
	ScheduleCron     ScheduleType = "cron"
)

// ScopeType selects which accounts a plan tests. The scope is resolved at the
// start of every run, so accounts added to a group later are picked up.
type ScopeType string

const (
	ScopeAll      ScopeType = "all"
	ScopeGroups   ScopeType = "groups"
	ScopeAccounts ScopeType = "accounts"
)

const (
	minIntervalMinutes = 10
	maxIntervalMinutes = 60 * 24 * 30
	maxScopeAccounts   = 5000
)

// Plan is one scheduled set of quality checks over a set of accounts.
type Plan struct {
	ID      int64         `json:"id"`
	Name    string        `json:"name"`
	Enabled bool          `json:"enabled"`
	Checks  []CheckConfig `json:"checks"`

	ScheduleType    ScheduleType `json:"schedule_type"`
	IntervalMinutes int          `json:"interval_minutes,omitempty"`
	CronExpr        string       `json:"cron_expr,omitempty"`

	ScopeType  ScopeType `json:"scope_type"`
	GroupIDs   []int64   `json:"group_ids,omitempty"`
	AccountIDs []int64   `json:"account_ids,omitempty"`
	Platforms  []string  `json:"platforms,omitempty"` // optional extra filter

	Policy            Policy `json:"policy"`
	MaxConcurrency    int    `json:"max_concurrency"`      // accounts tested in parallel, default 3
	MaxRequestsPerRun int    `json:"max_requests_per_run"` // 0 = unlimited
	JitterSeconds     int    `json:"jitter_seconds"`       // random delay before each request, default 5
	KeepResults       int    `json:"keep_results"`         // per account and check, default 50

	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Normalize validates the plan and fills defaults. It does not touch the
// schedule timestamps.
func (p *Plan) Normalize() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New("name is required")
	}
	if len(p.Name) > 100 {
		return errors.New("name is too long (max 100)")
	}

	if len(p.Checks) == 0 {
		return errors.New("at least one check is required")
	}
	seen := map[TestKind]bool{}
	for i := range p.Checks {
		c := &p.Checks[i]
		switch c.Kind {
		case TestCandy, TestPelicanSVG:
		default:
			return fmt.Errorf("unknown check kind %q", c.Kind)
		}
		if seen[c.Kind] {
			return fmt.Errorf("duplicate check %q", c.Kind)
		}
		seen[c.Kind] = true
		if len(c.Prompt) > 8000 {
			return errors.New("prompt is too long (max 8000 bytes)")
		}
		if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 900 {
			return errors.New("timeout_seconds must be between 0 and 900")
		}
		if c.MaxTokens < 0 || c.MaxTokens > 64000 {
			return errors.New("max_tokens must be between 0 and 64000")
		}
		switch c.CandyMode {
		case "", CandyModeAnywhere, CandyModeFinal:
		default:
			return fmt.Errorf("unknown candy_mode %q", c.CandyMode)
		}
	}

	switch p.ScheduleType {
	case ScheduleCron:
		p.CronExpr = strings.TrimSpace(p.CronExpr)
		if _, err := cron.ParseStandard(p.CronExpr); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
		p.IntervalMinutes = 0
	case ScheduleInterval, "":
		p.ScheduleType = ScheduleInterval
		if p.IntervalMinutes == 0 {
			p.IntervalMinutes = 360
		}
		if p.IntervalMinutes < minIntervalMinutes || p.IntervalMinutes > maxIntervalMinutes {
			return fmt.Errorf("interval_minutes must be between %d and %d", minIntervalMinutes, maxIntervalMinutes)
		}
		p.CronExpr = ""
	default:
		return fmt.Errorf("unknown schedule_type %q", p.ScheduleType)
	}

	switch p.ScopeType {
	case ScopeGroups:
		if len(p.GroupIDs) == 0 {
			return errors.New("group_ids is required for scope_type=groups")
		}
		p.AccountIDs = nil
	case ScopeAccounts:
		if len(p.AccountIDs) == 0 {
			return errors.New("account_ids is required for scope_type=accounts")
		}
		if len(p.AccountIDs) > maxScopeAccounts {
			return fmt.Errorf("too many accounts (max %d)", maxScopeAccounts)
		}
		p.GroupIDs = nil
	case ScopeAll, "":
		p.ScopeType = ScopeAll
		p.GroupIDs, p.AccountIDs = nil, nil
	default:
		return fmt.Errorf("unknown scope_type %q", p.ScopeType)
	}
	for i, pf := range p.Platforms {
		p.Platforms[i] = strings.ToLower(strings.TrimSpace(pf))
	}

	switch p.Policy.Mode {
	case "", PolicyConsecutive, PolicyPassRate:
	default:
		return fmt.Errorf("unknown policy mode %q", p.Policy.Mode)
	}
	if p.Policy.MinPassRate < 0 || p.Policy.MinPassRate > 1 {
		return errors.New("policy.min_pass_rate must be between 0 and 1")
	}
	p.Policy = p.Policy.withDefaults()

	p.MaxConcurrency = clampInt(p.MaxConcurrency, 3, 1, 20)
	p.JitterSeconds = clampInt(p.JitterSeconds, 5, 0, 300)
	p.KeepResults = clampInt(p.KeepResults, 50, 5, 1000)
	if p.MaxRequestsPerRun < 0 {
		p.MaxRequestsPerRun = 0
	}
	return nil
}

// ComputeNext returns the first run time after from.
func (p *Plan) ComputeNext(from time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	if p.ScheduleType == ScheduleCron {
		sched, err := cron.ParseStandard(p.CronExpr)
		if err != nil {
			return time.Time{}, err
		}
		return sched.Next(from.In(loc)), nil
	}
	if p.IntervalMinutes < 1 {
		return time.Time{}, errors.New("invalid interval")
	}
	return from.Add(time.Duration(p.IntervalMinutes) * time.Minute), nil
}

// clampInt returns def for 0, otherwise v limited to [lo, hi].
func clampInt(v, def, lo, hi int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
