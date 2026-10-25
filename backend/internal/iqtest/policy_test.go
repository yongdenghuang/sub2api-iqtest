package iqtest

import (
	"testing"
	"time"
)

var (
	rPass  = Result{Outcome: OutcomePass}
	rFail  = Result{Outcome: OutcomeQualityFail, Reason: ReasonAnswerMismatch}
	rInfra = Result{Outcome: OutcomeInfraError, Reason: ReasonRateLimited}
	rSkip  = Result{Outcome: OutcomeSkipped, Reason: ReasonCoolingDown}
)

func feed(p Policy, rs ...Result) (AccountState, []Transition) {
	var st AccountState
	var trs []Transition
	now := time.Unix(1_700_000_000, 0)
	for i, r := range rs {
		var tr Transition
		st, tr = p.Apply(st, r, now.Add(time.Duration(i)*time.Minute))
		trs = append(trs, tr)
	}
	return st, trs
}

func TestPolicyConsecutiveFlagsOnThird(t *testing.T) {
	st, trs := feed(Policy{}, rFail, rFail, rFail)
	if st.State != StateFlagged || st.FlaggedAt == nil {
		t.Fatalf("want flagged, got %+v", st)
	}
	if trs[0].To != StateSuspect || trs[1].To != StateSuspect || !trs[2].Changed || trs[2].To != StateFlagged {
		t.Fatalf("unexpected transitions: %+v", trs)
	}
}

func TestPolicyInfraAndSkipAreNeutral(t *testing.T) {
	st, _ := feed(Policy{}, rFail, rInfra, rSkip, rFail)
	if st.ConsecutiveFails != 2 || st.State != StateSuspect {
		t.Fatalf("infra/skip must neither count nor reset: %+v", st)
	}
	st, _ = feed(Policy{}, rFail, rInfra, rFail, rInfra, rFail)
	if st.State != StateFlagged {
		t.Fatalf("three quality failures separated by infra errors must flag: %+v", st)
	}
	st, _ = feed(Policy{}, rInfra, rInfra, rInfra, rInfra)
	if st.State != StateNormal || len(st.Recent) != 0 {
		t.Fatalf("only infra errors must never flag: %+v", st)
	}
}

func TestPolicyPassClearsSuspect(t *testing.T) {
	st, _ := feed(Policy{}, rFail, rFail, rPass)
	if st.State != StateNormal || st.ConsecutiveFails != 0 {
		t.Fatalf("a pass must clear suspect: %+v", st)
	}
}

func TestPolicyRecovery(t *testing.T) {
	st, _ := feed(Policy{}, rFail, rFail, rFail, rPass)
	if st.State != StateFlagged {
		t.Fatalf("one pass must not recover: %+v", st)
	}
	st, trs := feed(Policy{}, rFail, rFail, rFail, rPass, rPass)
	if st.State != StateNormal || st.FlaggedAt != nil || !trs[4].Changed {
		t.Fatalf("two passes must recover: %+v %+v", st, trs)
	}
	if len(st.Recent) != 2 {
		t.Fatalf("history should restart after recovery, got %v", st.Recent)
	}
}

func TestPolicyFailWhileFlaggedStaysFlagged(t *testing.T) {
	st, trs := feed(Policy{}, rFail, rFail, rFail, rFail, rPass, rFail)
	if st.State != StateFlagged {
		t.Fatalf("want flagged, got %+v", st)
	}
	if trs[3].Changed || trs[5].Changed {
		t.Fatalf("no transition expected while staying flagged: %+v", trs)
	}
}

func TestPolicyPassRate(t *testing.T) {
	p := Policy{Mode: PolicyPassRate, Window: 6, MinSamples: 4, MinPassRate: 0.5}
	st, _ := feed(p, rFail, rFail, rFail)
	if st.State != StateSuspect {
		t.Fatalf("below MinSamples must not flag: %+v", st)
	}
	st, _ = feed(p, rFail, rFail, rFail, rPass)
	if st.State != StateFlagged {
		t.Fatalf("25%% pass rate over 4 samples must flag: %+v", st)
	}
	st, _ = feed(p, rPass, rPass, rFail, rFail)
	if st.State == StateFlagged {
		t.Fatalf("50%% pass rate is not below the threshold: %+v", st)
	}
}

func TestPolicyWindowTrim(t *testing.T) {
	st, _ := feed(Policy{Window: 3}, rPass, rPass, rPass, rPass, rPass)
	if len(st.Recent) != 3 {
		t.Fatalf("window not applied: %v", st.Recent)
	}
}

func TestPolicyDoesNotMutateInput(t *testing.T) {
	in := AccountState{State: StateNormal, Recent: []bool{true}}
	out, _ := Policy{}.Apply(in, rFail, time.Now())
	if len(in.Recent) != 1 || len(out.Recent) != 2 {
		t.Fatalf("input mutated: in=%v out=%v", in.Recent, out.Recent)
	}
}
