package carpool

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
)

type QuotaReader interface {
	QueryUsage(context.Context, int64) (*service.OpenAIQuotaUsage, error)
}
type GroupInvalidator interface{ InvalidateCarpoolGroup(context.Context, int64) }
type SubscriptionInvalidator interface {
	InvalidateCarpoolSubscriptions(context.Context, int64, []int64) error
}

type Service struct {
	db            *sql.DB
	quota         QuotaReader
	groups        GroupInvalidator
	subscriptions SubscriptionInvalidator
	stop          chan struct{}
	once          sync.Once
	managed       sync.Map
	policyMu      sync.Mutex
}

type Config struct {
	Enabled           bool    `json:"enabled"`
	InitialTotalUSD   float64 `json:"initial_total_usd"`
	AllocationMode    string  `json:"allocation_mode,omitempty"`
	ManualWeeklyUSD   float64 `json:"manual_weekly_usd"`
	SyncOfficialReset *bool   `json:"sync_official_reset,omitempty"`
}
type Member struct {
	ID     int64      `json:"id"`
	UserID int64      `json:"user_id"`
	Name   string     `json:"name"`
	Usage  float64    `json:"usage"`
	Window *time.Time `json:"window"`
}
type Group struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	AccountID    int64    `json:"account_id"`
	AccountName  string   `json:"account_name"`
	AccountCount int      `json:"account_count"`
	Eligible     bool     `json:"eligible"`
	WeeklyLimit  float64  `json:"weekly_limit"`
	Config       Config   `json:"config"`
	State        State    `json:"state"`
	Members      []Member `json:"members"`
}

const schema = `CREATE TABLE IF NOT EXISTS carpool_groups (
 group_id BIGINT PRIMARY KEY REFERENCES groups(id), account_id BIGINT NOT NULL REFERENCES accounts(id),
 enabled BOOLEAN NOT NULL DEFAULT false, initial_total_usd NUMERIC(20,8) NOT NULL CHECK(initial_total_usd>0),
 state JSONB NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
 ALTER TABLE carpool_groups ADD COLUMN IF NOT EXISTS allocation_mode TEXT NOT NULL DEFAULT 'auto' CHECK(allocation_mode IN ('auto','manual'));
 ALTER TABLE carpool_groups ADD COLUMN IF NOT EXISTS sync_official_reset BOOLEAN NOT NULL DEFAULT true;
 CREATE UNIQUE INDEX IF NOT EXISTS carpool_enabled_account ON carpool_groups(account_id) WHERE enabled;
 CREATE TABLE IF NOT EXISTS carpool_events (id BIGSERIAL PRIMARY KEY,group_id BIGINT NOT NULL,
 kind TEXT NOT NULL, detail JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
 CREATE INDEX IF NOT EXISTS carpool_events_group ON carpool_events(group_id,id DESC);`

func Setup(cfg *config.Config, quota QuotaReader, groups GroupInvalidator, subscriptions SubscriptionInvalidator) (*Service, error) {
	if cfg == nil || quota == nil || groups == nil || subscriptions == nil {
		return nil, errors.New("carpool: missing dependencies")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	s := &Service{db: db, quota: quota, groups: groups, subscriptions: subscriptions, stop: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = s.refreshManaged(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Deployment verification disables credential use and all monetary writes.
	if cfg.TokenRefresh.Enabled {
		go s.loop()
	}
	return s, nil
}

func (s *Service) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7392060)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Stop() { s.once.Do(func() { close(s.stop) }) }
func (s *Service) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}
func (s *Service) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if err := s.refreshManaged(ctx); err != nil {
		slog.Warn("carpool: refresh configuration failed", "error", err)
		return
	}
	rows, err := s.db.QueryContext(ctx, "SELECT group_id FROM carpool_groups WHERE enabled ORDER BY updated_at,group_id LIMIT 20")
	if err != nil {
		slog.Warn("carpool: list failed", "error", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err = s.Sync(ctx, id); err != nil {
			slog.Warn("carpool: synchronization failed", "group_id", id, "error", err)
		}
	}
}

// ManagesWeeklyWindow keeps ordinary rolling-window maintenance from issuing
// a new weekly allowance before the upstream reset has been confirmed.
func (s *Service) ManagesWeeklyWindow(id int64) bool {
	_, ok := s.managed.Load(id)
	return ok
}

func (s *Service) refreshManaged(ctx context.Context) error {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	rows, err := s.db.QueryContext(ctx, "SELECT group_id FROM carpool_groups WHERE enabled AND sync_official_reset")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		ids[id] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for id := range ids {
		s.managed.Store(id, true)
	}
	s.managed.Range(func(key, _ any) bool {
		id, ok := key.(int64)
		if ok && !ids[id] {
			s.managed.Delete(key)
		}
		return true
	})
	return nil
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadGroup(ctx context.Context, q querier, id int64) (Group, error) {
	g := Group{Members: []Member{}}
	err := q.QueryRowContext(ctx, `SELECT g.id,g.name,COALESCE(g.weekly_limit_usd,0),
 g.deleted_at IS NULL AND g.status='active' AND g.platform='openai' AND g.subscription_type='subscription',
 (SELECT count(*) FROM account_groups ag JOIN accounts a ON a.id=ag.account_id WHERE ag.group_id=g.id AND a.deleted_at IS NULL),
 COALESCE((SELECT min(a.id) FROM account_groups ag JOIN accounts a ON a.id=ag.account_id WHERE ag.group_id=g.id AND a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL),0)
 FROM groups g WHERE g.id=$1`, id).Scan(&g.ID, &g.Name, &g.WeeklyLimit, &g.Eligible, &g.AccountCount, &g.AccountID)
	if err != nil {
		return g, err
	}
	if g.AccountID > 0 {
		if err = q.QueryRowContext(ctx, "SELECT name FROM accounts WHERE id=$1", g.AccountID).Scan(&g.AccountName); err != nil {
			return g, err
		}
	}
	var data []byte
	var syncReset bool
	err = q.QueryRowContext(ctx, "SELECT enabled,initial_total_usd,allocation_mode,sync_official_reset,state FROM carpool_groups WHERE group_id=$1", id).Scan(&g.Config.Enabled, &g.Config.InitialTotalUSD, &g.Config.AllocationMode, &syncReset, &data)
	if err == nil {
		g.Config.SyncOfficialReset = &syncReset
	}
	// The group's weekly cap is the single source of truth in manual mode.
	// Native group edits must not be overwritten by a second stored copy.
	g.Config.ManualWeeklyUSD = g.WeeklyLimit
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return g, err
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &g.State); err != nil {
			return g, err
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT s.id,s.user_id,COALESCE(NULLIF(u.username,''),u.email),s.weekly_usage_usd,s.weekly_window_start
 FROM user_subscriptions s JOIN users u ON u.id=s.user_id WHERE s.group_id=$1 AND s.deleted_at IS NULL
 AND s.status='active' AND s.starts_at<=now() AND s.expires_at>now() AND u.deleted_at IS NULL AND u.status='active' ORDER BY s.id`, id)
	if err != nil {
		return g, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.ID, &m.UserID, &m.Name, &m.Usage, &m.Window); err != nil {
			return g, err
		}
		g.Members = append(g.Members, m)
	}
	if err = rows.Err(); err != nil {
		return g, err
	}
	if g.Config.InitialTotalUSD <= 0 {
		g.Config.InitialTotalUSD = g.WeeklyLimit * float64(len(g.Members))
	}
	return g, nil
}

func (s *Service) List(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM groups WHERE (deleted_at IS NULL AND status='active' AND platform='openai' AND subscription_type='subscription') OR id IN (SELECT group_id FROM carpool_groups WHERE enabled) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	groups := make([]Group, 0, len(ids))
	for _, id := range ids {
		g, err := loadGroup(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (s *Service) lock(ctx context.Context, id int64) (*sql.Conn, func(), error) {
	if id <= 0 || id > 2147483647 {
		return nil, nil, errors.New("invalid group id")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	key := int64(739206000000) + id
	var ok bool
	err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok)
	if err != nil || !ok {
		_ = conn.Close()
		return nil, nil, errors.New("此分组正在同步，请稍后重试")
	}
	return conn, func() {
		unlock, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlock, "SELECT pg_advisory_unlock($1)", key); err != nil {
			// Never return a session with a held advisory lock to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}, nil
}

func (s *Service) Sync(ctx context.Context, id int64) (retErr error) {
	conn, release, err := s.lock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	g, err := loadGroup(ctx, conn, id)
	if err != nil {
		return err
	}
	if !g.Config.Enabled {
		return errors.New("请先启用此分组的额度同步")
	}
	defer func() {
		if retErr != nil {
			_, _ = conn.ExecContext(ctx, `UPDATE carpool_groups SET state=jsonb_set(state,'{last_error}',to_jsonb($2::text)),updated_at=now() WHERE group_id=$1`, id, retErr.Error())
		}
	}()
	var bound int64
	if err = conn.QueryRowContext(ctx, "SELECT account_id FROM carpool_groups WHERE group_id=$1", id).Scan(&bound); err != nil {
		return err
	}
	if !g.Eligible || g.AccountCount != 1 || g.AccountID != bound {
		return errors.New("上游账号绑定发生变化，已暂停自动调整")
	}
	var active bool
	if err = conn.QueryRowContext(ctx, "SELECT status='active' AND schedulable FROM accounts WHERE id=$1", bound).Scan(&active); err != nil {
		return err
	}
	if !active {
		return errors.New("上游账号异常或调度已停用，保留当前额度")
	}
	if len(g.Members) == 0 {
		return errors.New("没有生效车友，保留当前额度")
	}
	usage, err := s.quota.QueryUsage(ctx, bound)
	if err != nil {
		return fmt.Errorf("读取官方额度失败: %w", err)
	}
	now := time.Now().UTC()
	percent, reset, err := weeklyObservation(usage, now)
	if err != nil {
		return err
	}
	st := g.State
	st.Windows = officialWindows(usage, now)
	if g.Config.automaticAllocation() && st.Capacity <= 0 {
		st.Capacity = g.Config.InitialTotalUSD
	}
	resetChanged, ready := observeWindow(&st, percent, reset, now)
	if !ready {
		return saveState(ctx, conn, id, st)
	}
	return s.apply(ctx, conn, g, st, resetChanged, now)
}

func saveState(ctx context.Context, conn *sql.Conn, id int64, st State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "UPDATE carpool_groups SET state=$2,updated_at=now() WHERE group_id=$1", id, data)
	return err
}

func (s *Service) apply(ctx context.Context, conn *sql.Conn, observed Group, st State, resetChanged bool, now time.Time) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize against ordinary subscription billing before reading counters.
	rows, err := tx.QueryContext(ctx, "SELECT id FROM user_subscriptions WHERE group_id=$1 AND deleted_at IS NULL ORDER BY id FOR UPDATE", observed.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	var gid int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM groups WHERE id=$1 FOR UPDATE", observed.ID).Scan(&gid); err != nil {
		return err
	}
	g, err := loadGroup(ctx, tx, observed.ID)
	if err != nil {
		return err
	}
	if !g.Eligible || g.AccountCount != 1 || g.AccountID != observed.AccountID || !g.Config.Enabled {
		return errors.New("同步期间配置变化，未修改额度")
	}
	if len(g.Members) == 0 {
		return errors.New("没有生效车友，未修改额度")
	}
	start := st.ResetAt.Add(-week)
	if start.After(now) {
		return errors.New("官方周期起点在未来，未修改额度")
	}
	before := append([]Member(nil), g.Members...)
	memberUsage := make([]float64, 0, len(g.Members))
	changed := resetChanged && g.Config.syncOfficialReset()
	for i := range g.Members {
		m := &g.Members[i]
		if g.Config.syncOfficialReset() && (m.Window == nil || !m.Window.Equal(start)) {
			// Subtract only old charges proven by retained request logs. Pending
			// billing and post-reset requests remain in the counter; missing logs
			// cannot accidentally grant extra quota. No wallet/history is altered.
			if m.Window != nil && start.After(*m.Window) {
				var old float64
				if err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(actual_cost),0) FROM usage_logs WHERE subscription_id=$1 AND created_at >= $2 AND created_at < $3`, m.ID, *m.Window, start).Scan(&old); err != nil {
					return err
				}
				m.Usage = mathMax(0, m.Usage-old)
			}
			if m.Window == nil {
				// Preserve existing day/month counters and anchors; initialize only
				// missing anchors so later native window maintenance still works.
				if _, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_window_start=COALESCE(daily_window_start,$2),monthly_window_start=COALESCE(monthly_window_start,$3) WHERE id=$1`, m.ID, timezone.StartOfDay(now), now); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, "UPDATE user_subscriptions SET weekly_window_start=$2,weekly_usage_usd=$3,updated_at=now() WHERE id=$1", m.ID, start, m.Usage); err != nil {
				return err
			}
			m.Window = &start
			changed = true
		}
		memberUsage = append(memberUsage, m.Usage)
	}
	limit := g.WeeklyLimit
	if g.Config.automaticAllocation() {
		limit, err = estimateGroupLimit(ctx, tx, g, &st, memberUsage, start, now)
		if err != nil {
			return err
		}
	} else {
		st.Estimated, st.Capacity, st.Baseline = false, 0, Observation{}
		st.Message = "手动额度已生效，不随官方用量或人数自动调整"
	}
	if !finite(limit) || limit <= 0 {
		return errors.New("周额度无效，请设置大于零的每人周上限")
	}
	if g.Config.automaticAllocation() && absFloat(limit-g.WeeklyLimit) > 0.000000001 {
		if _, err = tx.ExecContext(ctx, "UPDATE groups SET weekly_limit_usd=$2,updated_at=now() WHERE id=$1", g.ID, limit); err != nil {
			return err
		}
		changed = true
	}
	st.Members, st.PerMemberUSD = len(g.Members), limit
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE carpool_groups SET state=$2,updated_at=now() WHERE group_id=$1", g.ID, data); err != nil {
		return err
	}
	if changed {
		kind := "adjust"
		if resetChanged && g.Config.syncOfficialReset() {
			kind = "official_reset"
		}
		detail, err := json.Marshal(map[string]any{"before_limit": g.WeeklyLimit, "after_limit": limit, "before_members": before, "after_members": g.Members, "state": st})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO carpool_events(group_id,kind,detail) VALUES($1,$2,$3)", g.ID, kind, detail); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.invalidateGroup(ctx, g)
}

func estimateGroupLimit(ctx context.Context, q querier, g Group, st *State, memberUsage []float64, start, now time.Time) (float64, error) {
	// Only site-routed traffic has dollar costs. Direct Codex/ChatGPT use can
	// distort this estimate, which is why automatic allocation is optional.
	var cost, multiplier float64
	if err := q.QueryRowContext(ctx, "SELECT rate_multiplier FROM groups WHERE id=$1", g.ID).Scan(&multiplier); err != nil {
		return 0, err
	}
	if !finite(multiplier) || multiplier <= 0 {
		return 0, errors.New("免费或异常倍率分组无法测算美元额度")
	}
	if err := q.QueryRowContext(ctx, "SELECT COALESCE(sum(total_cost),0) FROM usage_logs WHERE account_id=$1 AND created_at >= $2", g.AccountID, start).Scan(&cost); err != nil {
		return 0, err
	}
	estimate(st, Observation{At: now, Percent: st.Percent, Cost: cost * multiplier})
	// Before enough samples exist, departed subscribers' spending still counts
	// against the seed pool. Rejoining users retain their existing counters.
	var excludedUsage float64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN s.weekly_window_start >= $2 THEN s.weekly_usage_usd ELSE
 (SELECT COALESCE(sum(l.actual_cost),0) FROM usage_logs l WHERE l.subscription_id=s.id AND l.created_at >= $2) END),0)
 FROM user_subscriptions s JOIN users u ON u.id=s.user_id WHERE s.group_id=$1 AND NOT
 (s.deleted_at IS NULL AND s.status='active' AND s.starts_at<=now() AND s.expires_at>now() AND u.deleted_at IS NULL AND u.status='active')`, g.ID, start).Scan(&excludedUsage); err != nil {
		return 0, err
	}
	return fairLimit(*st, memberUsage, excludedUsage), nil
}

func (s *Service) invalidateGroup(ctx context.Context, g Group) error {
	ids := make([]int64, 0, len(g.Members))
	for _, m := range g.Members {
		ids = append(ids, m.UserID)
	}
	// Retry invalidation on every successful poll, including after a prior
	// committed update whose cache publication failed.
	s.groups.InvalidateCarpoolGroup(ctx, g.ID)
	return s.subscriptions.InvalidateCarpoolSubscriptions(ctx, g.ID, ids)
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
