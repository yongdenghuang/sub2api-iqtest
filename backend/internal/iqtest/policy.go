package iqtest

import "time"

// PolicyMode selects how an account becomes flagged.
type PolicyMode string

const (
	PolicyConsecutive PolicyMode = "consecutive" // N quality failures in a row
	PolicyPassRate    PolicyMode = "pass_rate"   // pass rate over the last Window valid samples
)

// FlagState is the quality state of one account for one check.
type FlagState string

const (
	StateNormal  FlagState = "normal"
	StateSuspect FlagState = "suspect" // at least one recent quality failure, not flagged yet
	StateFlagged FlagState = "flagged"
)

// Policy decides when an account is flagged and when it recovers.
// Only pass and quality_fail results count; infra errors and skips are neutral.
type Policy struct {
	Mode             PolicyMode `json:"mode"`
	ConsecutiveFails int        `json:"consecutive_fails"` // consecutive mode, default 3
	Window           int        `json:"window"`            // valid samples kept, default 10
	MinSamples       int        `json:"min_samples"`       // pass_rate mode, default 5
	MinPassRate      float64    `json:"min_pass_rate"`     // pass_rate mode, default 0.5
	RecoverPasses    int        `json:"recover_passes"`    // consecutive passes to clear a flag, default 2
}

func (p Policy) withDefaults() Policy {
	if p.Mode != PolicyPassRate {
		p.Mode = PolicyConsecutive
	}
	if p.ConsecutiveFails <= 0 {
		p.ConsecutiveFails = 3
	}
	if p.Window <= 0 {
		p.Window = 10
	}
	if p.MinSamples <= 0 {
		p.MinSamples = 5
	}
	if p.MinSamples > p.Window {
		p.MinSamples = p.Window
	}
	if p.MinPassRate <= 0 {
		p.MinPassRate = 0.5
	}
	if p.RecoverPasses <= 0 {
		p.RecoverPasses = 2
	}
	return p
}

// AccountState is the persisted quality state of one account for one check.
type AccountState struct {
	State             FlagState  `json:"state"`
	ConsecutiveFails  int        `json:"consecutive_fails"`
	ConsecutivePasses int        `json:"consecutive_passes"`
	Recent            []bool     `json:"recent"` // newest last; true = pass
	LastOutcome       Outcome    `json:"last_outcome"`
	LastReason        Reason     `json:"last_reason,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
	FlaggedAt         *time.Time `json:"flagged_at,omitempty"`
}

// Transition reports a state change caused by one result.
type Transition struct {
	From    FlagState
	To      FlagState
	Changed bool
}

// Apply folds one result into the state. It never mutates the input state.
func (p Policy) Apply(st AccountState, r Result, now time.Time) (AccountState, Transition) {
	p = p.withDefaults()
	if st.State == "" {
		st.State = StateNormal
	}
	from := st.State
	st.Recent = append([]bool(nil), st.Recent...)
	st.LastOutcome, st.LastReason, st.UpdatedAt = r.Outcome, r.Reason, now

	if !r.Outcome.CountsForQuality() {
		return st, Transition{From: from, To: from}
	}

	if r.Outcome == OutcomePass {
		st.ConsecutiveFails = 0
		st.ConsecutivePasses++
		st.Recent = append(st.Recent, true)
	} else {
		st.ConsecutivePasses = 0
		st.ConsecutiveFails++
		st.Recent = append(st.Recent, false)
	}
	if len(st.Recent) > p.Window {
		st.Recent = st.Recent[len(st.Recent)-p.Window:]
	}

	switch {
	case from == StateFlagged:
		if st.ConsecutivePasses >= p.RecoverPasses {
			st.State = StateNormal
			// Start fresh so old failures cannot immediately re-flag the account.
			n := min(st.ConsecutivePasses, len(st.Recent))
			st.Recent = st.Recent[len(st.Recent)-n:]
		}
	case p.tripped(st):
		st.State = StateFlagged
	case st.ConsecutiveFails > 0:
		st.State = StateSuspect
	default:
		st.State = StateNormal
	}

	switch {
	case st.State == StateFlagged && from != StateFlagged:
		t := now
		st.FlaggedAt = &t
	case st.State != StateFlagged:
		st.FlaggedAt = nil
	}
	return st, Transition{From: from, To: st.State, Changed: from != st.State}
}

func (p Policy) tripped(st AccountState) bool {
	if p.Mode == PolicyPassRate {
		if len(st.Recent) < p.MinSamples {
			return false
		}
		pass := 0
		for _, ok := range st.Recent {
			if ok {
				pass++
			}
		}
		return float64(pass)/float64(len(st.Recent)) < p.MinPassRate
	}
	return st.ConsecutiveFails >= p.ConsecutiveFails
}
