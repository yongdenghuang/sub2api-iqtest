package iqtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ---- fakes ----

type memRepo struct {
	mu      sync.Mutex
	results []*ResultRecord
	states  map[string]*StateRow
	events  []*Event
	runs    int
}

func newMemRepo() *memRepo { return &memRepo{states: map[string]*StateRow{}} }

func skey(id int64, k TestKind) string { return string(k) + "/" + string(rune(id+'0')) }

func (m *memRepo) CreateRun(_ context.Context, _ int64, _ string, _ int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs++
	return int64(m.runs), nil
}
func (m *memRepo) FinishRun(context.Context, int64, string, RunStats) error { return nil }
func (m *memRepo) InsertResult(_ context.Context, r *ResultRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = append(m.results, r)
	return nil
}
func (m *memRepo) PruneResults(context.Context, int64, TestKind, int) error { return nil }
func (m *memRepo) GetState(_ context.Context, id int64, k TestKind) (AccountState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.states[skey(id, k)]; ok {
		return r.AccountState, true, nil
	}
	return AccountState{State: StateNormal}, false, nil
}
func (m *memRepo) UpsertState(_ context.Context, row *StateRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *row
	m.states[skey(row.AccountID, row.Kind)] = &cp
	return nil
}
func (m *memRepo) InsertEvent(_ context.Context, e *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

type fakeAccounts struct{ list []*service.Account }

func (f fakeAccounts) ListAccounts(_ context.Context, page, _ int, _, _, _, _ string, _ int64, _ string, _, _ string) ([]service.Account, int64, error) {
	if page > 1 {
		return nil, int64(len(f.list)), nil
	}
	out := make([]service.Account, 0, len(f.list))
	for _, a := range f.list {
		out = append(out, *a)
	}
	return out, int64(len(f.list)), nil
}
func (f fakeAccounts) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	var out []*service.Account
	for _, id := range ids {
		for _, a := range f.list {
			if a.ID == id {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

func acct(id int64) *service.Account {
	return &service.Account{ID: id, Name: "acc", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}
}

// reply builds a tester that answers with a fixed text and records which accounts it was asked.
func reply(text string, called *[]int64, mu *sync.Mutex) fakeTester {
	return fakeTester{fn: func(c *gin.Context, id int64, model, prompt string) error {
		mu.Lock()
		*called = append(*called, id)
		mu.Unlock()
		emit(c, service.TestEvent{Type: "test_start", Model: model})
		emit(c, service.TestEvent{Type: "content", Text: text})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}
}

func newPlan() *Plan {
	p := &Plan{ID: 1, Name: "t", Checks: []CheckConfig{{Kind: TestCandy}}, ScopeType: ScopeAll}
	_ = p.Normalize()
	p.JitterSeconds = 0
	return p
}

func newTestEngine(repo Repo, accts []*service.Account, tester AccountTester) *Engine {
	e := NewEngine(repo, fakeAccounts{list: accts}, tester)
	e.sleep = func(context.Context, time.Duration) bool { return true }
	return e
}

func runOnce(t *testing.T, e *Engine, p *Plan) RunStats {
	t.Helper()
	pr, err := e.Prepare(context.Background(), p, "test")
	if err != nil {
		t.Fatal(err)
	}
	return e.Execute(context.Background(), pr)
}

// ---- tests ----

func TestEnginePinsEveryRequestToItsAccount(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	e := newTestEngine(newMemRepo(), []*service.Account{acct(1), acct(2), acct(3)}, reply("答案是21", &called, &mu))
	st := runOnce(t, e, newPlan())
	if st.Pass != 3 || st.Requests != 3 {
		t.Fatalf("stats %+v", st)
	}
	seen := map[int64]int{}
	for _, id := range called {
		seen[id]++
	}
	if len(seen) != 3 || seen[1] != 1 || seen[2] != 1 || seen[3] != 1 {
		t.Fatalf("each account must be asked exactly once, got %v", seen)
	}
}

func TestEngineScopeAccountsOnlyTestsThose(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	e := newTestEngine(newMemRepo(), []*service.Account{acct(1), acct(2), acct(3)}, reply("21", &called, &mu))
	p := newPlan()
	p.ScopeType, p.AccountIDs = ScopeAccounts, []int64{2}
	runOnce(t, e, p)
	if len(called) != 1 || called[0] != 2 {
		t.Fatalf("called %v", called)
	}
}

func TestEngineSkipsCoolingDownAccount(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	hot := acct(2)
	future := time.Now().Add(time.Hour)
	hot.RateLimitResetAt = &future
	repo := newMemRepo()
	e := newTestEngine(repo, []*service.Account{acct(1), hot}, reply("21", &called, &mu))
	st := runOnce(t, e, newPlan())
	if st.Skipped != 1 || st.Pass != 1 || st.Requests != 1 {
		t.Fatalf("stats %+v", st)
	}
	if len(called) != 1 || called[0] != 1 {
		t.Fatalf("cooling account must not be called: %v", called)
	}
}

func TestEngineRequestBudget(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	e := newTestEngine(newMemRepo(), []*service.Account{acct(1), acct(2), acct(3)}, reply("21", &called, &mu))
	p := newPlan()
	p.MaxRequestsPerRun = 1
	p.MaxConcurrency = 1
	st := runOnce(t, e, p)
	if st.Requests != 1 || st.Skipped != 2 || len(called) != 1 {
		t.Fatalf("stats %+v called %v", st, called)
	}
}

func TestEngineThreeQualityFailuresFlag(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	repo := newMemRepo()
	e := newTestEngine(repo, []*service.Account{acct(1)}, reply("答案是29", &called, &mu))
	p := newPlan()
	for i := 0; i < 3; i++ {
		st := runOnce(t, e, p)
		if st.QualityFail != 1 {
			t.Fatalf("run %d stats %+v", i, st)
		}
	}
	row := repo.states[skey(1, TestCandy)]
	if row == nil || !row.Flagged || row.State != StateFlagged {
		t.Fatalf("expected flagged, got %+v", row)
	}
	if len(repo.events) == 0 {
		t.Fatalf("expected a state-change event")
	}
}

func TestEngineInfraErrorsNeverFlag(t *testing.T) {
	tester := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "error", Error: "API returned 429: rate limit exceeded"})
		return errors.New("failed")
	}}
	repo := newMemRepo()
	e := newTestEngine(repo, []*service.Account{acct(1)}, tester)
	p := newPlan()
	for i := 0; i < 6; i++ {
		st := runOnce(t, e, p)
		if st.InfraError != 1 || st.QualityFail != 0 {
			t.Fatalf("run %d stats %+v", i, st)
		}
	}
	if row := repo.states[skey(1, TestCandy)]; row != nil && (row.Flagged || row.State != StateNormal) {
		t.Fatalf("infra errors must never flag: %+v", row)
	}
	for _, r := range repo.results {
		if r.Outcome.CountsForQuality() {
			t.Fatalf("infra result counted for quality: %+v", r.Result)
		}
	}
}

func TestEnginePlatformFilter(t *testing.T) {
	var called []int64
	var mu sync.Mutex
	other := acct(2)
	other.Platform = service.PlatformOpenAI
	e := newTestEngine(newMemRepo(), []*service.Account{acct(1), other}, reply("21", &called, &mu))
	p := newPlan()
	p.Platforms = []string{"anthropic"}
	runOnce(t, e, p)
	if len(called) != 1 || called[0] != 1 {
		t.Fatalf("called %v", called)
	}
}

// Persisted legacy plans must not turn an invalid group (0 means all groups
// to AccountSource) or an unknown scope into a request against the whole pool.
func TestResolveAccountsRejectsInvalidStoredScope(t *testing.T) {
	e := NewEngine(newMemRepo(), nil, nil) // a query would panic: validation must happen first
	for _, p := range []*Plan{
		{ScopeType: ScopeGroups, GroupIDs: []int64{0}},
		{ScopeType: ScopeGroups, GroupIDs: []int64{-1}},
		{ScopeType: ScopeGroups},
		{ScopeType: ScopeAccounts, AccountIDs: []int64{0}},
		{ScopeType: ScopeAccounts},
		{ScopeType: "typo"},
	} {
		if _, err := e.ResolveAccounts(context.Background(), p); err == nil {
			t.Errorf("accepted invalid scope: %+v", p)
		}
	}
}
