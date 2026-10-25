package iqtest

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReasonBudgetExhausted is declared in model.go; maxStoredOutput caps what we keep per result.
const maxStoredOutput = 300 << 10

// AccountSource is satisfied by service.AdminService.
type AccountSource interface {
	ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode string, sortBy, sortOrder string) ([]service.Account, int64, error)
	GetAccountsByIDs(ctx context.Context, ids []int64) ([]*service.Account, error)
}

// Repo is the persistence the engine needs. *Store implements it.
type Repo interface {
	CreateRun(ctx context.Context, planID int64, trigger string, accounts int) (int64, error)
	FinishRun(ctx context.Context, id int64, status string, st RunStats) error
	InsertResult(ctx context.Context, r *ResultRecord) error
	PruneResults(ctx context.Context, accountID int64, kind TestKind, keep int) error
	GetState(ctx context.Context, accountID int64, kind TestKind) (AccountState, bool, error)
	UpsertState(ctx context.Context, row *StateRow) error
	InsertEvent(ctx context.Context, e *Event) error
}

// RunStats are the counters of one run.
type RunStats struct {
	Accounts    int `json:"accounts"`
	Requests    int `json:"requests"` // checks that actually reached an upstream
	Pass        int `json:"pass"`
	QualityFail int `json:"quality_fail"`
	InfraError  int `json:"infra_error"`
	Skipped     int `json:"skipped"`
}

// Prepared is a run whose accounts are resolved and whose row exists.
type Prepared struct {
	Plan     *Plan
	RunID    int64
	Trigger  string
	Accounts []*service.Account
	reserved int64 // request budget counter
}

// Engine runs plans against accounts. It never picks or switches accounts on its own.
type Engine struct {
	repo     Repo
	accounts AccountSource
	tester   AccountTester
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) bool
}

func NewEngine(repo Repo, accounts AccountSource, tester AccountTester) *Engine {
	return &Engine{repo: repo, accounts: accounts, tester: tester, now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ResolveAccounts expands the plan scope into concrete accounts, in id order.
// "all" and "groups" cover active accounts only; explicitly listed accounts are
// included whatever their state, and are recorded as skipped when unusable.
func (e *Engine) ResolveAccounts(ctx context.Context, p *Plan) ([]*service.Account, error) {
	var out []*service.Account
	seen := map[int64]bool{}
	add := func(a *service.Account) {
		if a == nil || seen[a.ID] {
			return
		}
		if len(p.Platforms) > 0 && !containsFold(p.Platforms, a.Platform) {
			return
		}
		seen[a.ID] = true
		out = append(out, a)
	}

	switch p.ScopeType {
	case ScopeAccounts:
		if len(p.AccountIDs) == 0 {
			return nil, fmt.Errorf("account_ids is required")
		}
		if err := validateScopeIDs(p.AccountIDs, "account_ids"); err != nil {
			return nil, err
		}
		list, err := e.accounts.GetAccountsByIDs(ctx, p.AccountIDs)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			add(a)
		}
	case ScopeGroups:
		if len(p.GroupIDs) == 0 {
			return nil, fmt.Errorf("group_ids is required")
		}
		if err := validateScopeIDs(p.GroupIDs, "group_ids"); err != nil {
			return nil, err
		}
		for _, gid := range p.GroupIDs {
			if err := e.listAll(ctx, gid, add); err != nil {
				return nil, err
			}
		}
	case ScopeAll:
		if err := e.listAll(ctx, 0, add); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown scope_type %q", p.ScopeType)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (e *Engine) listAll(ctx context.Context, groupID int64, add func(*service.Account)) error {
	const pageSize = 200
	for page := 1; ; page++ {
		items, total, err := e.accounts.ListAccounts(ctx, page, pageSize, "", "", service.StatusActive, "", groupID, "", "", "")
		if err != nil {
			return err
		}
		for i := range items {
			a := items[i]
			add(&a)
		}
		if len(items) == 0 || int64(page*pageSize) >= total {
			return nil
		}
	}
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// Prepare resolves accounts and creates the run row.
func (e *Engine) Prepare(ctx context.Context, p *Plan, trigger string) (*Prepared, error) {
	accounts, err := e.ResolveAccounts(ctx, p)
	if err != nil {
		return nil, err
	}
	runID, err := e.repo.CreateRun(ctx, p.ID, trigger, len(accounts))
	if err != nil {
		return nil, err
	}
	return &Prepared{Plan: p, RunID: runID, Trigger: trigger, Accounts: accounts}, nil
}

// Execute runs every check on every prepared account and finishes the run row.
func (e *Engine) Execute(ctx context.Context, pr *Prepared) RunStats {
	p := pr.Plan
	stats := RunStats{Accounts: len(pr.Accounts)}
	var mu sync.Mutex
	count := func(r Result) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Outcome {
		case OutcomePass:
			stats.Pass++
		case OutcomeQualityFail:
			stats.QualityFail++
		case OutcomeInfraError:
			stats.InfraError++
		default:
			stats.Skipped++
		}
		if r.Outcome != OutcomeSkipped {
			stats.Requests++
		}
	}

	sem := make(chan struct{}, p.MaxConcurrency)
	var wg sync.WaitGroup
loop:
	for _, acc := range pr.Accounts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(a *service.Account) {
			defer wg.Done()
			defer func() { <-sem }()
			e.runAccount(ctx, pr, a, count)
		}(acc)
	}
	wg.Wait()

	status := "done"
	if ctx.Err() != nil {
		status = "aborted"
	}
	fin, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	if err := e.repo.FinishRun(fin, pr.RunID, status, stats); err != nil {
		slog.Warn("iqtest: finish run failed", "run_id", pr.RunID, "error", err)
	}
	return stats
}

func (e *Engine) runAccount(ctx context.Context, pr *Prepared, a *service.Account, count func(Result)) {
	for _, chk := range pr.Plan.Checks {
		if ctx.Err() != nil {
			return
		}
		res := e.checkOne(ctx, pr, a, chk)
		if res.Outcome == OutcomeInfraError && res.Reason == ReasonCancelled && ctx.Err() != nil {
			return // shutting down: do not record half-finished work
		}
		count(res)
		e.record(ctx, pr, a, res)
	}
}

func (e *Engine) checkOne(ctx context.Context, pr *Prepared, a *service.Account, chk CheckConfig) Result {
	p := pr.Plan
	skip := func(r Reason, d string) Result {
		return Result{Kind: chk.Kind, Outcome: OutcomeSkipped, Reason: r, Detail: d}
	}
	if el := CheckEligibility(a, e.now()); !el.OK {
		return skip(el.Reason, el.Detail)
	}
	if n := atomic.AddInt64(&pr.reserved, 1); p.MaxRequestsPerRun > 0 && n > int64(p.MaxRequestsPerRun) {
		return skip(ReasonBudgetExhausted, "request budget of this run is used up")
	}
	if p.JitterSeconds > 0 {
		d := time.Duration(rand.Int63n(int64(p.JitterSeconds)*int64(time.Second) + 1))
		if !e.sleep(ctx, d) {
			return Result{Kind: chk.Kind, Outcome: OutcomeInfraError, Reason: ReasonCancelled, Detail: "run cancelled"}
		}
	}
	return RunCheck(ctx, e.tester, a.ID, chk)
}

func (e *Engine) record(ctx context.Context, pr *Prepared, a *service.Account, res Result) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()

	rec := &ResultRecord{
		RunID: pr.RunID, PlanID: pr.Plan.ID,
		AccountID: a.ID, AccountName: a.Name, Platform: a.Platform,
		Result: res,
	}
	rec.Detail = sanitize(rec.Detail, 2000)
	rec.Output = sanitize(rec.Output, maxStoredOutput)
	if err := e.repo.InsertResult(bg, rec); err != nil {
		slog.Warn("iqtest: insert result failed", "account_id", a.ID, "error", err)
	}
	if err := e.repo.PruneResults(bg, a.ID, res.Kind, pr.Plan.KeepResults); err != nil {
		slog.Warn("iqtest: prune results failed", "account_id", a.ID, "error", err)
	}

	st, _, err := e.repo.GetState(bg, a.ID, res.Kind)
	if err != nil {
		slog.Warn("iqtest: load state failed", "account_id", a.ID, "error", err)
		return
	}
	next, tr := pr.Plan.Policy.Apply(st, res, e.now())
	row := &StateRow{
		AccountID: a.ID, AccountName: a.Name, Platform: a.Platform,
		Kind: res.Kind, PlanID: pr.Plan.ID,
		AccountState: next, Flagged: next.State == StateFlagged,
	}
	if err := e.repo.UpsertState(bg, row); err != nil {
		slog.Warn("iqtest: save state failed", "account_id", a.ID, "error", err)
		return
	}
	if tr.Changed {
		ev := &Event{
			AccountID: a.ID, AccountName: a.Name, Kind: res.Kind, PlanID: pr.Plan.ID,
			From: string(tr.From), To: string(tr.To), Detail: sanitize(res.Detail, 500),
		}
		if err := e.repo.InsertEvent(bg, ev); err != nil {
			slog.Warn("iqtest: insert event failed", "account_id", a.ID, "error", err)
		}
		slog.Info("iqtest: account state changed",
			"account_id", a.ID, "kind", res.Kind, "from", tr.From, "to", tr.To)
	}
}

// sanitize makes text safe for PostgreSQL (no NUL bytes, valid UTF-8) and bounded.
func sanitize(s string, max int) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) > max {
		s = s[:max]
	}
	return strings.ToValidUTF8(s, "")
}
