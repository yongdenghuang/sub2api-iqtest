package iqtest

import (
	"testing"
	"time"
)

func validPlan() Plan {
	return Plan{Name: "p", Checks: []CheckConfig{{Kind: TestCandy}}}
}

func TestNormalizeDefaults(t *testing.T) {
	p := validPlan()
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	if p.ScheduleType != ScheduleInterval || p.IntervalMinutes != 360 {
		t.Fatalf("schedule defaults: %s %d", p.ScheduleType, p.IntervalMinutes)
	}
	if p.ScopeType != ScopeAll || p.MaxConcurrency != 3 || p.JitterSeconds != 5 || p.KeepResults != 50 {
		t.Fatalf("defaults: %+v", p)
	}
	if p.Policy.Mode != PolicyConsecutive || p.Policy.ConsecutiveFails != 3 {
		t.Fatalf("policy defaults: %+v", p.Policy)
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := map[string]func(p *Plan){
		"name":        func(p *Plan) { p.Name = "  " },
		"no checks":   func(p *Plan) { p.Checks = nil },
		"bad kind":    func(p *Plan) { p.Checks[0].Kind = "nope" },
		"dup kind":    func(p *Plan) { p.Checks = append(p.Checks, CheckConfig{Kind: TestCandy}) },
		"interval lo": func(p *Plan) { p.IntervalMinutes = 5 },
		"bad cron":    func(p *Plan) { p.ScheduleType, p.CronExpr = ScheduleCron, "not a cron" },
		"groups":      func(p *Plan) { p.ScopeType = ScopeGroups },
		"accounts":    func(p *Plan) { p.ScopeType = ScopeAccounts },
		"bad scope":   func(p *Plan) { p.ScopeType = "x" },
		"bad mode":    func(p *Plan) { p.Policy.Mode = "x" },
		"bad rate":    func(p *Plan) { p.Policy.MinPassRate = 1.5 },
		"timeout":     func(p *Plan) { p.Checks[0].TimeoutSeconds = 5000 },
	}
	for name, mut := range cases {
		p := validPlan()
		mut(&p)
		if err := p.Normalize(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeScopeCleanup(t *testing.T) {
	p := validPlan()
	p.ScopeType = ScopeGroups
	p.GroupIDs = []int64{1}
	p.AccountIDs = []int64{9}
	p.Platforms = []string{" Anthropic "}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	if p.AccountIDs != nil || p.Platforms[0] != "anthropic" {
		t.Fatalf("not cleaned: %+v", p)
	}
}

func TestComputeNextInterval(t *testing.T) {
	p := validPlan()
	p.IntervalMinutes = 30
	p.ScheduleType = ScheduleInterval
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := p.ComputeNext(from, time.UTC)
	if err != nil || !got.Equal(from.Add(30*time.Minute)) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestComputeNextCronTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata not available")
	}
	p := validPlan()
	p.ScheduleType = ScheduleCron
	p.CronExpr = "0 3 * * *" // 03:00 Shanghai = 19:00 UTC the day before
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := p.ComputeNext(from, loc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got.UTC(), want)
	}
	p.CronExpr = "bad"
	if _, err := p.ComputeNext(from, loc); err == nil {
		t.Fatalf("expected error for bad cron")
	}
}

func TestNormalizeRejectsUnsafeScopeAndCron(t *testing.T) {
	for _, id := range []int64{0, -1} {
		for _, scope := range []ScopeType{ScopeAccounts, ScopeGroups} {
			p := validPlan()
			p.ScopeType, p.AccountIDs, p.GroupIDs = scope, []int64{id}, []int64{id}
			if err := p.Normalize(); err == nil {
				t.Errorf("accepted %s ID %d", scope, id)
			}
		}
	}
	for _, expr := range []string{"@every 1s", "@daily", "CRON_TZ=UTC 0 3 * * *", "0 0 3 * * *"} {
		p := validPlan()
		p.ScheduleType, p.CronExpr = ScheduleCron, expr
		if err := p.Normalize(); err == nil {
			t.Errorf("accepted %q", expr)
		}
	}
}

func TestComputeNextImpossibleCron(t *testing.T) {
	p := validPlan()
	p.ScheduleType, p.CronExpr = ScheduleCron, "0 0 31 2 *"
	if _, err := p.ComputeNext(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); err == nil {
		t.Fatal("February 31 must not produce a zero next-run timestamp")
	}
}

func TestComputeNextLeapDay(t *testing.T) {
	p := validPlan()
	p.ScheduleType, p.CronExpr = ScheduleCron, "0 0 29 2 *"
	next, err := p.ComputeNext(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil || next.Year() != 2028 || next.Month() != time.February || next.Day() != 29 {
		t.Fatalf("valid leap-day schedule: next=%v err=%v", next, err)
	}
}
