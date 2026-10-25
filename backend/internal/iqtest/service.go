package iqtest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// Deps are the few things iqtest needs from the host application.
type Deps struct {
	Cfg      *config.Config
	Redis    *redis.Client // optional
	Accounts AccountSource
	Tester   AccountTester
}

// Service is the running iqtest feature.
type Service struct {
	store  *Store
	engine *Engine
	sched  *Scheduler
	loc    *time.Location
}

// Setup connects to the database, applies our own migrations and starts the scheduler.
func Setup(d Deps) (*Service, error) {
	if d.Cfg == nil || d.Accounts == nil || d.Tester == nil {
		return nil, errors.New("iqtest: missing dependencies")
	}
	store, err := OpenStore(d.Cfg.Database.DSN())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.AbortStaleRuns(ctx); err != nil {
		slog.Warn("iqtest: abort stale runs failed", "error", err)
	}

	loc := time.Local
	if d.Cfg.Timezone != "" {
		if l, err := time.LoadLocation(d.Cfg.Timezone); err == nil {
			loc = l
		}
	}
	engine := NewEngine(store, d.Accounts, d.Tester)
	sched := NewScheduler(store, engine, d.Redis, loc)
	sched.Start()
	return &Service{store: store, engine: engine, sched: sched, loc: loc}, nil
}

// Stop halts the scheduler. In-flight runs end with the process.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.sched.Stop()
}
