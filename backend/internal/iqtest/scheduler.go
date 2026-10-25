package iqtest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

// ErrAlreadyRunning means the plan is still executing (here or on another instance).
var ErrAlreadyRunning = errors.New("iqtest: plan is already running")

var unlockScript = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)

// Scheduler wakes up every minute and starts the plans that are due.
type Scheduler struct {
	store  *Store
	engine *Engine
	rdb    *redis.Client // optional: serialises plans across instances
	loc    *time.Location

	cron      *cron.Cron
	running   sync.Map // plan id -> struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewScheduler(store *Store, engine *Engine, rdb *redis.Client, loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.Local
	}
	return &Scheduler{store: store, engine: engine, rdb: rdb, loc: loc}
}

func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		c := cron.New(cron.WithLocation(s.loc))
		if _, err := c.AddFunc("* * * * *", s.tick); err != nil {
			slog.Error("iqtest: scheduler not started", "error", err)
			return
		}
		s.cron = c
		c.Start()
		slog.Info("iqtest: scheduler started (tick every minute)")
	})
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.cron != nil {
			select {
			case <-s.cron.Stop().Done():
			case <-time.After(3 * time.Second):
			}
		}
	})
}

func (s *Scheduler) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	plans, err := s.store.ListDuePlans(ctx, time.Now())
	if err != nil {
		slog.Warn("iqtest: list due plans failed", "error", err)
		return
	}
	for _, p := range plans {
		s.launch(ctx, p)
	}
}

// launch advances the schedule first (so a slow or failing run can never cause
// a hot loop), then starts the run unless the plan is still busy.
func (s *Scheduler) launch(ctx context.Context, p *Plan) {
	now := time.Now()
	var next *time.Time
	if n, err := p.ComputeNext(now, s.loc); err != nil {
		slog.Warn("iqtest: cannot compute next run, leaving plan unscheduled", "plan_id", p.ID, "error", err)
		if saveErr := s.store.SetPlanNextRun(ctx, p.ID, nil); saveErr != nil {
			slog.Warn("iqtest: clear invalid schedule failed", "plan_id", p.ID, "error", saveErr)
		}
		return
	} else {
		next = &n
	}
	if err := s.store.SetPlanNextRun(ctx, p.ID, next); err != nil {
		slog.Warn("iqtest: save next run failed", "plan_id", p.ID, "error", err)
		return
	}
	runID, err := s.start(ctx, p, "schedule")
	switch {
	case errors.Is(err, ErrAlreadyRunning):
		slog.Info("iqtest: previous run still active, skipping this cycle", "plan_id", p.ID)
	case err != nil:
		slog.Warn("iqtest: start run failed", "plan_id", p.ID, "error", err)
	default:
		_ = s.store.SetPlanLastRun(ctx, p.ID, now)
		slog.Info("iqtest: run started", "plan_id", p.ID, "run_id", runID)
	}
}

// RunNow starts a persisted plan immediately without changing its schedule.
func (s *Scheduler) RunNow(ctx context.Context, p *Plan) (int64, error) {
	runID, err := s.start(ctx, p, "manual")
	if err == nil {
		_ = s.store.SetPlanLastRun(ctx, p.ID, time.Now())
	}
	return runID, err
}

// RunAdHoc starts a one-off run that is not tied to a stored plan.
func (s *Scheduler) RunAdHoc(ctx context.Context, p *Plan) (int64, error) {
	pr, err := s.engine.Prepare(ctx, p, "adhoc")
	if err != nil {
		return 0, err
	}
	go s.engine.Execute(context.Background(), pr)
	return pr.RunID, nil
}

func (s *Scheduler) start(ctx context.Context, p *Plan, trigger string) (int64, error) {
	if _, loaded := s.running.LoadOrStore(p.ID, struct{}{}); loaded {
		return 0, ErrAlreadyRunning
	}
	release, ok := s.lock(ctx, p.ID)
	if !ok {
		s.running.Delete(p.ID)
		return 0, ErrAlreadyRunning
	}
	finish := func() {
		release()
		s.running.Delete(p.ID)
	}
	pr, err := s.engine.Prepare(ctx, p, trigger)
	if err != nil {
		finish()
		return 0, err
	}
	go func() {
		defer finish()
		s.engine.Execute(context.Background(), pr)
	}()
	return pr.RunID, nil
}

// lock takes a per-plan Redis lock so only one instance runs a plan at a time.
// If Redis is unavailable it fails open: the in-process guard still applies.
func (s *Scheduler) lock(ctx context.Context, planID int64) (release func(), ok bool) {
	noop := func() {}
	if s.rdb == nil {
		return noop, true
	}
	key := fmt.Sprintf("iqtest:plan:%d", planID)
	token := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
	got, err := s.rdb.SetNX(ctx, key, token, 3*time.Hour).Result()
	if err != nil {
		slog.Warn("iqtest: redis lock unavailable, continuing without it", "error", err)
		return noop, true
	}
	if !got {
		return nil, false
	}
	return func() {
		_, _ = unlockScript.Run(context.Background(), s.rdb, []string{key}, token).Result()
	}, true
}
