package carpool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const (
	AllocationAuto   = "auto"
	AllocationManual = "manual"
)

func (c Config) automaticAllocation() bool { return c.AllocationMode != AllocationManual }
func (c Config) syncOfficialReset() bool {
	return c.SyncOfficialReset == nil || *c.SyncOfficialReset
}

func normalizeConfig(cfg Config, current Group) (Config, error) {
	if cfg.AllocationMode == "" {
		cfg.AllocationMode = current.Config.AllocationMode
		if cfg.AllocationMode == "" {
			// Older clients explicitly opted into the combined automatic switch.
			cfg.AllocationMode = AllocationAuto
		}
		if cfg.ManualWeeklyUSD == 0 {
			cfg.ManualWeeklyUSD = current.WeeklyLimit
		}
	}
	if cfg.SyncOfficialReset == nil {
		reset := current.Config.syncOfficialReset()
		cfg.SyncOfficialReset = &reset
	}
	switch cfg.AllocationMode {
	case AllocationManual:
		if !validBudget(cfg.ManualWeeklyUSD) {
			return cfg, errors.New("每人周额度须为 1 至 1000000 美元")
		}
		if !validBudget(cfg.InitialTotalUSD) {
			cfg.InitialTotalUSD = math.Min(1000000, cfg.ManualWeeklyUSD*float64(max(1, len(current.Members))))
		}
	case AllocationAuto:
		if !validBudget(cfg.InitialTotalUSD) {
			return cfg, errors.New("初始总周额度须为 1 至 1000000 美元")
		}
	default:
		return cfg, errors.New("请选择手动额度或动态均分")
	}
	return cfg, nil
}

func validBudget(v float64) bool { return finite(v) && v >= 1 && v <= 1000000 }

// Save applies manual limits immediately, without querying the upstream or
// resetting any subscriber's spent quota/window. Sync only observes the group
// cap in manual mode, so edits from native group management remain effective.
func (s *Service) Save(ctx context.Context, id int64, cfg Config) error {
	conn, release, err := s.lock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	// Disabling remains possible even after the source/group became invalid.
	if !cfg.Enabled {
		_, err = conn.ExecContext(ctx, "UPDATE carpool_groups SET enabled=false,updated_at=now() WHERE group_id=$1", id)
		if err == nil {
			s.setManaged(id, false)
		}
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var gid int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM groups WHERE id=$1 FOR UPDATE", id).Scan(&gid); err != nil {
		return err
	}
	g, err := loadGroup(ctx, tx, id)
	if err != nil {
		return err
	}
	if !g.Eligible || g.AccountCount != 1 || g.AccountID == 0 {
		return errors.New("请选择只绑定一个 OpenAI OAuth 上游账号的订阅分组")
	}
	if len(g.Members) == 0 {
		return errors.New("分组没有生效的订阅用户")
	}
	cfg, err = normalizeConfig(cfg, g)
	if err != nil {
		return err
	}
	var previousAccount int64
	err = tx.QueryRowContext(ctx, "SELECT account_id FROM carpool_groups WHERE group_id=$1", id).Scan(&previousAccount)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previousAccount > 0 && previousAccount != g.AccountID {
		return errors.New("上游绑定已变化，需新建独立订阅分组以保留原周期记录")
	}
	st, afterLimit, kind := g.State, g.WeeklyLimit, "config"
	if !cfg.automaticAllocation() {
		afterLimit, kind = cfg.ManualWeeklyUSD, "manual_limit"
		if _, err = tx.ExecContext(ctx, "UPDATE groups SET weekly_limit_usd=$2,updated_at=now() WHERE id=$1", id, afterLimit); err != nil {
			return err
		}
		st.Capacity, st.Estimated, st.Baseline = 0, false, Observation{}
		st.Members, st.PerMemberUSD = len(g.Members), afterLimit
		st.Message = "手动额度已生效，不随官方用量或人数自动调整"
	} else if !g.Config.automaticAllocation() || !st.Estimated {
		// Explicitly re-enabling automatic allocation starts fresh samples,
		// instead of reusing an estimate distorted by off-site consumption.
		st.Capacity = cfg.InitialTotalUSD
		if !g.Config.automaticAllocation() {
			st.Estimated, st.Baseline = false, Observation{}
			st.Message = "已开启动态均分，等待新的官方用量样本"
		}
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO carpool_groups(group_id,account_id,enabled,initial_total_usd,allocation_mode,sync_official_reset,state)
 VALUES($1,$2,true,$3,$4,$5,$6) ON CONFLICT(group_id) DO UPDATE SET enabled=true,
 initial_total_usd=excluded.initial_total_usd,allocation_mode=excluded.allocation_mode,
 sync_official_reset=excluded.sync_official_reset,state=excluded.state,updated_at=now()`,
		id, g.AccountID, cfg.InitialTotalUSD, cfg.AllocationMode, cfg.syncOfficialReset(), data)
	if err != nil {
		return fmt.Errorf("保存失败；同一上游账号只能启用一个拼车额度池: %w", err)
	}
	detail, err := json.Marshal(map[string]any{"before_limit": g.WeeklyLimit, "after_limit": afterLimit, "before_config": g.Config, "after_config": cfg})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO carpool_events(group_id,kind,detail) VALUES($1,$2,$3)", id, kind, detail); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.setManaged(id, cfg.syncOfficialReset())
	return s.invalidateGroup(ctx, g)
}

func (s *Service) setManaged(id int64, managed bool) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if managed {
		s.managed.Store(id, true)
	} else {
		s.managed.Delete(id)
	}
}
