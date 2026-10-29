package iqtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq" // PostgreSQL driver (already a dependency of the project)
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("iqtest: not found")

// migrations are applied in order and recorded in iq_schema_migrations, a table
// of our own, so they never collide with the official migration numbering.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS iq_plans (
		id          BIGSERIAL PRIMARY KEY,
		name        TEXT NOT NULL,
		enabled     BOOLEAN NOT NULL DEFAULT FALSE,
		config      JSONB NOT NULL,
		next_run_at TIMESTAMPTZ,
		last_run_at TIMESTAMPTZ,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX IF NOT EXISTS iq_plans_due_idx ON iq_plans (next_run_at) WHERE enabled;

	CREATE TABLE IF NOT EXISTS iq_runs (
		id           BIGSERIAL PRIMARY KEY,
		plan_id      BIGINT NOT NULL DEFAULT 0,
		trigger      TEXT NOT NULL,
		status       TEXT NOT NULL,
		started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		finished_at  TIMESTAMPTZ,
		accounts     INT NOT NULL DEFAULT 0,
		requests     INT NOT NULL DEFAULT 0,
		pass         INT NOT NULL DEFAULT 0,
		quality_fail INT NOT NULL DEFAULT 0,
		infra_error  INT NOT NULL DEFAULT 0,
		skipped      INT NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS iq_runs_plan_idx ON iq_runs (plan_id, id DESC);

	CREATE TABLE IF NOT EXISTS iq_results (
		id           BIGSERIAL PRIMARY KEY,
		run_id       BIGINT NOT NULL,
		plan_id      BIGINT NOT NULL DEFAULT 0,
		account_id   BIGINT NOT NULL,
		account_name TEXT NOT NULL DEFAULT '',
		platform     TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL,
		outcome      TEXT NOT NULL,
		reason       TEXT NOT NULL DEFAULT '',
		detail       TEXT NOT NULL DEFAULT '',
		output       TEXT NOT NULL DEFAULT '',
		score        INT NOT NULL DEFAULT 0,
		metrics      JSONB,
		duration_ms  BIGINT NOT NULL DEFAULT 0,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX IF NOT EXISTS iq_results_account_idx ON iq_results (account_id, kind, id DESC);
	CREATE INDEX IF NOT EXISTS iq_results_run_idx ON iq_results (run_id);

	CREATE TABLE IF NOT EXISTS iq_account_state (
		account_id   BIGINT NOT NULL,
		kind         TEXT NOT NULL,
		account_name TEXT NOT NULL DEFAULT '',
		platform     TEXT NOT NULL DEFAULT '',
		plan_id      BIGINT NOT NULL DEFAULT 0,
		state        JSONB NOT NULL,
		flagged      BOOLEAN NOT NULL DEFAULT FALSE,
		updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (account_id, kind)
	);
	CREATE INDEX IF NOT EXISTS iq_account_state_flagged_idx ON iq_account_state (flagged);

	CREATE TABLE IF NOT EXISTS iq_events (
		id           BIGSERIAL PRIMARY KEY,
		account_id   BIGINT NOT NULL,
		account_name TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL,
		plan_id      BIGINT NOT NULL DEFAULT 0,
		from_state   TEXT NOT NULL,
		to_state     TEXT NOT NULL,
		detail       TEXT NOT NULL DEFAULT '',
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX IF NOT EXISTS iq_events_idx ON iq_events (id DESC);`,
	`CREATE TABLE IF NOT EXISTS iq_account_display_order (
		account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
		position BIGINT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS iq_account_display_order_position_idx ON iq_account_display_order (position, account_id);`,
	`-- Historical static passes are not visual quality verdicts.
 WITH legacy AS (
  UPDATE iq_results SET outcome='skipped', reason='visual_review_required', score=0,
   metrics=COALESCE(NULLIF(metrics,'null'::jsonb),'{}'::jsonb) || jsonb_build_object('legacy_structure_score',score,'requests',1),
   detail='Historical SVG structure check only; visual review required'
  WHERE kind='pelican_svg' AND outcome='pass'
   AND COALESCE(metrics->>'evaluation_version','') <> 'pelican-vision-v1'
  RETURNING run_id
 ), counts AS (SELECT run_id,COUNT(*) AS n FROM legacy GROUP BY run_id)
 UPDATE iq_runs SET pass=GREATEST(0,pass-counts.n), skipped=skipped+counts.n FROM counts WHERE iq_runs.id=counts.run_id;
 UPDATE iq_account_state SET flagged=false, state=state || jsonb_build_object(
  'state','normal','consecutive_fails',0,'consecutive_passes',0,'recent','[]'::jsonb,'flagged_at',NULL,
  'last_outcome',CASE WHEN state->>'last_outcome'='pass' THEN 'skipped' ELSE state->>'last_outcome' END,
  'last_reason',CASE WHEN state->>'last_outcome'='pass' THEN 'visual_review_required' ELSE COALESCE(state->>'last_reason','') END
 ) WHERE kind='pelican_svg';`,
}

const migrationLockKey = 7392011

// Store is the PostgreSQL persistence of the iqtest feature.
type Store struct{ db *sql.DB }

var _ Repo = (*Store)(nil)

func OpenStore(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Migrate applies pending migrations; an advisory lock serialises instances.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iq_schema_migrations (
		version INT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for i, m := range migrations {
		version := i + 1
		var n int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM iq_schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("iqtest migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iq_schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// AbortStaleRuns closes runs left "running" by a crashed process.
func (s *Store) AbortStaleRuns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE iq_runs SET status = 'aborted', finished_at = now()
		WHERE status = 'running' AND started_at < now() - interval '3 hours'`)
	return err
}

// ---------- plans ----------

const planCols = `id, name, enabled, config, next_run_at, last_run_at, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanPlan(r rowScanner) (*Plan, error) {
	var (
		id           int64
		name         string
		enabled      bool
		cfg          []byte
		next, last   sql.NullTime
		created, upd time.Time
	)
	if err := r.Scan(&id, &name, &enabled, &cfg, &next, &last, &created, &upd); err != nil {
		return nil, err
	}
	p := &Plan{}
	if err := json.Unmarshal(cfg, p); err != nil {
		return nil, fmt.Errorf("plan %d: bad config: %w", id, err)
	}
	p.ID, p.Name, p.Enabled = id, name, enabled
	p.CreatedAt, p.UpdatedAt = created, upd
	p.NextRunAt, p.LastRunAt = nil, nil
	if next.Valid {
		t := next.Time
		p.NextRunAt = &t
	}
	if last.Valid {
		t := last.Time
		p.LastRunAt = &t
	}
	return p, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func (s *Store) CreatePlan(ctx context.Context, p *Plan) error {
	cfg, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO iq_plans (name, enabled, config, next_run_at) VALUES ($1, $2, $3::jsonb, $4)
		 RETURNING id, created_at, updated_at`,
		p.Name, p.Enabled, string(cfg), nullTime(p.NextRunAt)).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Store) UpdatePlan(ctx context.Context, p *Plan) error {
	cfg, err := json.Marshal(p)
	if err != nil {
		return err
	}
	err = s.db.QueryRowContext(ctx,
		`UPDATE iq_plans SET name = $2, enabled = $3, config = $4::jsonb, next_run_at = $5, updated_at = now()
		 WHERE id = $1 RETURNING updated_at`,
		p.ID, p.Name, p.Enabled, string(cfg), nullTime(p.NextRunAt)).Scan(&p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM iq_plans WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetPlan(ctx context.Context, id int64) (*Plan, error) {
	p, err := scanPlan(s.db.QueryRowContext(ctx, `SELECT `+planCols+` FROM iq_plans WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) ListPlans(ctx context.Context) ([]*Plan, error) {
	return s.queryPlans(ctx, `SELECT `+planCols+` FROM iq_plans ORDER BY id`)
}

func (s *Store) ListDuePlans(ctx context.Context, now time.Time) ([]*Plan, error) {
	return s.queryPlans(ctx, `SELECT `+planCols+` FROM iq_plans
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1 ORDER BY next_run_at`, now)
}

func (s *Store) queryPlans(ctx context.Context, q string, args ...any) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetPlanNextRun(ctx context.Context, id int64, next *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE iq_plans SET next_run_at = $2 WHERE id = $1`, id, nullTime(next))
	return err
}

func (s *Store) SetPlanLastRun(ctx context.Context, id int64, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE iq_plans SET last_run_at = $2 WHERE id = $1`, id, t)
	return err
}

// ---------- runs ----------

// Run is one execution of a plan.
type Run struct {
	ID         int64      `json:"id"`
	PlanID     int64      `json:"plan_id"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	RunStats
}

func (s *Store) CreateRun(ctx context.Context, planID int64, trigger string, accounts int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO iq_runs (plan_id, trigger, status, accounts) VALUES ($1, $2, 'running', $3) RETURNING id`,
		planID, trigger, accounts).Scan(&id)
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, id int64, status string, st RunStats) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE iq_runs SET status = $2, finished_at = now(), accounts = $3, requests = $4,
		 pass = $5, quality_fail = $6, infra_error = $7, skipped = $8 WHERE id = $1`,
		id, status, st.Accounts, st.Requests, st.Pass, st.QualityFail, st.InfraError, st.Skipped)
	return err
}

const runCols = `id, plan_id, trigger, status, started_at, finished_at, accounts, requests, pass, quality_fail, infra_error, skipped`

func scanRun(r rowScanner) (*Run, error) {
	var run Run
	var fin sql.NullTime
	if err := r.Scan(&run.ID, &run.PlanID, &run.Trigger, &run.Status, &run.StartedAt, &fin,
		&run.Accounts, &run.Requests, &run.Pass, &run.QualityFail, &run.InfraError, &run.Skipped); err != nil {
		return nil, err
	}
	if fin.Valid {
		t := fin.Time
		run.FinishedAt = &t
	}
	return &run, nil
}

func (s *Store) GetRun(ctx context.Context, id int64) (*Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM iq_runs WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err == nil && r.Status == "running" {
		err = s.db.QueryRowContext(ctx, `SELECT
   COALESCE(SUM(COALESCE((metrics->>'requests')::int, CASE WHEN outcome <> 'skipped' THEN 1 ELSE 0 END)),0), COUNT(*) FILTER (WHERE outcome = 'pass'),
   COUNT(*) FILTER (WHERE outcome = 'quality_fail'), COUNT(*) FILTER (WHERE outcome = 'infra_error'),
   COUNT(*) FILTER (WHERE outcome = 'skipped') FROM iq_results WHERE run_id = $1`, id).
			Scan(&r.Requests, &r.Pass, &r.QualityFail, &r.InfraError, &r.Skipped)
	}
	return r, err
}

// ListRuns lists runs newest first; planID < 0 means all plans.
func (s *Store) ListRuns(ctx context.Context, planID int64, limit, offset int) ([]*Run, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM iq_runs WHERE $1::bigint < 0 OR plan_id = $1::bigint`, planID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runCols+` FROM iq_runs WHERE $1::bigint < 0 OR plan_id = $1::bigint ORDER BY id DESC LIMIT $2 OFFSET $3`,
		planID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []*Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ---------- results ----------

// ResultRecord is a stored Result with its context.
type ResultRecord struct {
	ID          int64     `json:"id"`
	RunID       int64     `json:"run_id"`
	PlanID      int64     `json:"plan_id"`
	AccountID   int64     `json:"account_id"`
	AccountName string    `json:"account_name"`
	Platform    string    `json:"platform"`
	CreatedAt   time.Time `json:"created_at"`
	Result
}

func (s *Store) InsertResult(ctx context.Context, r *ResultRecord) error {
	metrics, err := json.Marshal(r.Metrics)
	if err != nil {
		metrics = []byte("null")
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO iq_results (run_id, plan_id, account_id, account_name, platform, kind, outcome, reason,
		 detail, output, score, metrics, duration_ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13)
		 RETURNING id, created_at`,
		r.RunID, r.PlanID, r.AccountID, r.AccountName, r.Platform, string(r.Kind), string(r.Outcome), string(r.Reason),
		r.Detail, r.Output, r.Score, string(metrics), r.DurationMs).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) PruneResults(ctx context.Context, accountID int64, kind TestKind, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM iq_results WHERE account_id = $1 AND kind = $2 AND id < (
		   SELECT id FROM iq_results WHERE account_id = $1 AND kind = $2 ORDER BY id DESC OFFSET $3 LIMIT 1)`,
		accountID, string(kind), keep-1)
	return err
}

const resultColsLite = `id, run_id, plan_id, account_id, account_name, platform, kind, outcome, reason, detail, '' AS output, score, metrics, duration_ms, created_at`
const resultColsFull = `id, run_id, plan_id, account_id, account_name, platform, kind, outcome, reason, detail, output, score, metrics, duration_ms, created_at`

func scanResult(r rowScanner) (*ResultRecord, error) {
	var rec ResultRecord
	var kind, outcome, reason string
	var metrics []byte
	if err := r.Scan(&rec.ID, &rec.RunID, &rec.PlanID, &rec.AccountID, &rec.AccountName, &rec.Platform,
		&kind, &outcome, &reason, &rec.Detail, &rec.Output, &rec.Score, &metrics, &rec.DurationMs, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.Kind, rec.Outcome, rec.Reason = TestKind(kind), Outcome(outcome), Reason(reason)
	if len(metrics) > 0 && string(metrics) != "null" {
		var m any
		if json.Unmarshal(metrics, &m) == nil {
			rec.Metrics = m
		}
	}
	return &rec, nil
}

func (s *Store) GetResult(ctx context.Context, id int64) (*ResultRecord, error) {
	r, err := scanResult(s.db.QueryRowContext(ctx, `SELECT `+resultColsFull+` FROM iq_results WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (s *Store) queryResults(ctx context.Context, q string, args ...any) ([]*ResultRecord, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*ResultRecord{}
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListResultsByRun omits the (possibly large) output column.
func (s *Store) ListResultsByRun(ctx context.Context, runID int64, limit, offset int) ([]*ResultRecord, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM iq_results WHERE run_id = $1`, runID).Scan(&total); err != nil {
		return nil, 0, err
	}
	out, err := s.queryResults(ctx,
		`SELECT `+resultColsLite+` FROM iq_results WHERE run_id = $1 ORDER BY account_id, id LIMIT $2 OFFSET $3`,
		runID, limit, offset)
	return out, total, err
}

// ListResultsByAccount lists one account's recent results, newest first; kind "" means all kinds.
func (s *Store) ListResultsByAccount(ctx context.Context, accountID int64, kind string, limit int) ([]*ResultRecord, error) {
	return s.queryResults(ctx,
		`SELECT `+resultColsLite+` FROM iq_results WHERE account_id = $1 AND ($2 = '' OR kind = $2)
		 ORDER BY id DESC LIMIT $3`, accountID, kind, limit)
}

// ---------- account state ----------

// StateRow is the persisted quality state of one account for one check.
type StateRow struct {
	AccountID   int64    `json:"account_id"`
	AccountName string   `json:"account_name"`
	Platform    string   `json:"platform"`
	Kind        TestKind `json:"kind"`
	PlanID      int64    `json:"plan_id"`
	Flagged     bool     `json:"flagged"`
	AccountState
}

func (s *Store) GetState(ctx context.Context, accountID int64, kind TestKind) (AccountState, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT state FROM iq_account_state WHERE account_id = $1 AND kind = $2`, accountID, string(kind)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountState{State: StateNormal}, false, nil
	}
	if err != nil {
		return AccountState{}, false, err
	}
	var st AccountState
	if err := json.Unmarshal(raw, &st); err != nil {
		return AccountState{}, false, err
	}
	return st, true, nil
}

func (s *Store) UpsertState(ctx context.Context, row *StateRow) error {
	raw, err := json.Marshal(row.AccountState)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO iq_account_state (account_id, kind, account_name, platform, plan_id, state, flagged, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7, now())
		 ON CONFLICT (account_id, kind) DO UPDATE SET account_name = EXCLUDED.account_name,
		   platform = EXCLUDED.platform, plan_id = EXCLUDED.plan_id, state = EXCLUDED.state,
		   flagged = EXCLUDED.flagged, updated_at = now()`,
		row.AccountID, string(row.Kind), row.AccountName, row.Platform, row.PlanID, string(raw), row.Flagged)
	return err
}

// ListStates lists account states, flagged first; kind "" means all kinds.
func (s *Store) ListStates(ctx context.Context, flaggedOnly bool, kind string, limit, offset int) ([]*StateRow, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM iq_account_state WHERE (NOT $1 OR flagged) AND ($2 = '' OR kind = $2)`,
		flaggedOnly, kind).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT account_id, kind, account_name, platform, plan_id, state, flagged FROM iq_account_state
		 WHERE (NOT $1 OR flagged) AND ($2 = '' OR kind = $2)
		 ORDER BY flagged DESC, updated_at DESC LIMIT $3 OFFSET $4`, flaggedOnly, kind, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out, err := scanStates(rows)
	return out, total, err
}

// ListStatesByAccounts returns every check state of the given accounts, for
// showing results next to the official account list.
func (s *Store) ListStatesByAccounts(ctx context.Context, accountIDs []int64) ([]*StateRow, error) {
	if len(accountIDs) == 0 {
		return []*StateRow{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT account_id, kind, account_name, platform, plan_id, state, flagged FROM iq_account_state
		 WHERE account_id = ANY($1) ORDER BY account_id, kind`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	return scanStates(rows)
}

func scanStates(rows *sql.Rows) ([]*StateRow, error) {
	defer func() { _ = rows.Close() }()
	out := []*StateRow{}
	for rows.Next() {
		var r StateRow
		var kindStr string
		var raw []byte
		if err := rows.Scan(&r.AccountID, &kindStr, &r.AccountName, &r.Platform, &r.PlanID, &raw, &r.Flagged); err != nil {
			return nil, err
		}
		r.Kind = TestKind(kindStr)
		if err := json.Unmarshal(raw, &r.AccountState); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ---------- events ----------

// Event records a flag / recover transition.
type Event struct {
	ID          int64     `json:"id"`
	AccountID   int64     `json:"account_id"`
	AccountName string    `json:"account_name"`
	Kind        TestKind  `json:"kind"`
	PlanID      int64     `json:"plan_id"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Detail      string    `json:"detail,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) InsertEvent(ctx context.Context, e *Event) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO iq_events (account_id, account_name, kind, plan_id, from_state, to_state, detail)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
		e.AccountID, e.AccountName, string(e.Kind), e.PlanID, e.From, e.To, e.Detail).Scan(&e.ID, &e.CreatedAt)
}

func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]*Event, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM iq_events`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account_id, account_name, kind, plan_id, from_state, to_state, detail, created_at
		 FROM iq_events ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []*Event{}
	for rows.Next() {
		var e Event
		var kind string
		if err := rows.Scan(&e.ID, &e.AccountID, &e.AccountName, &kind, &e.PlanID, &e.From, &e.To, &e.Detail, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		e.Kind = TestKind(kind)
		out = append(out, &e)
	}
	return out, total, rows.Err()
}
