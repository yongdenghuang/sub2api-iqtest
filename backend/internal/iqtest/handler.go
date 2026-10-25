package iqtest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
)

// RegisterRoutes mounts the admin API under /admin/iqtest.
// Pass the same authentication middleware the other admin routes use.
func (s *Service) RegisterRoutes(v1 *gin.RouterGroup, mw ...gin.HandlerFunc) {
	g := v1.Group("/admin/iqtest", mw...)
	g.GET("/defaults", s.getDefaults)

	g.GET("/plans", s.listPlans)
	g.POST("/plans", s.createPlan)
	g.PUT("/plans/:id", s.updatePlan)
	g.DELETE("/plans/:id", s.deletePlan)
	g.POST("/plans/:id/run", s.runPlan)

	g.POST("/runs/adhoc", s.runAdHoc)
	g.GET("/runs", s.listRuns)
	g.GET("/runs/:id", s.getRun)
	g.GET("/runs/:id/results", s.listRunResults)

	g.GET("/results/:id", s.getResult)
	g.GET("/accounts/:id/results", s.listAccountResults)
	g.POST("/accounts/:id/clear", s.clearAccountState)

	g.GET("/states", s.listStates)
	g.GET("/states/by-accounts", s.listStatesByAccounts)
	g.GET("/events", s.listEvents)
}

func paramID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid id")
		return 0, false
	}
	return id, true
}

func queryInt(c *gin.Context, name string, def, max int) int {
	v, err := strconv.Atoi(c.Query(name))
	if err != nil || v < 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func (s *Service) getDefaults(c *gin.Context) {
	response.Success(c, gin.H{
		"candy_prompt":         DefaultCandyPrompt,
		"pelican_prompt":       DefaultPelicanPrompt,
		"policy":               Policy{}.withDefaults(),
		"min_interval_minutes": minIntervalMinutes,
		"check_kinds":          []TestKind{TestCandy, TestPelicanSVG},
	})
}

// ---------- plans ----------

func (s *Service) listPlans(c *gin.Context) {
	plans, err := s.store.ListPlans(c.Request.Context())
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, plans)
}

func (s *Service) createPlan(c *gin.Context) {
	var p Plan
	if err := c.ShouldBindJSON(&p); err != nil {
		response.BadRequest(c, "invalid body: "+err.Error())
		return
	}
	p.ID = 0
	if err := p.Normalize(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	p.NextRunAt, p.LastRunAt = nil, nil
	if p.Enabled {
		next, err := p.ComputeNext(time.Now(), s.loc)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		p.NextRunAt = &next
	}
	if err := s.store.CreatePlan(c.Request.Context(), &p); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Created(c, p)
}

func (s *Service) updatePlan(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	old, err := s.store.GetPlan(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		response.NotFound(c, "plan not found")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	var p Plan
	if err := c.ShouldBindJSON(&p); err != nil {
		response.BadRequest(c, "invalid body: "+err.Error())
		return
	}
	p.ID = id
	if err := p.Normalize(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	p.CreatedAt, p.LastRunAt, p.NextRunAt = old.CreatedAt, old.LastRunAt, nil
	if p.Enabled {
		next, err := p.ComputeNext(time.Now(), s.loc)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		p.NextRunAt = &next
	}
	if err := s.store.UpdatePlan(c.Request.Context(), &p); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, p)
}

func (s *Service) deletePlan(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if err := s.store.DeletePlan(c.Request.Context(), id); errors.Is(err, ErrNotFound) {
		response.NotFound(c, "plan not found")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"deleted": id})
}

func (s *Service) runPlan(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	p, err := s.store.GetPlan(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		response.NotFound(c, "plan not found")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	runID, err := s.sched.RunNow(c.Request.Context(), p)
	if errors.Is(err, ErrAlreadyRunning) {
		response.Error(c, 409, "plan is already running")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Accepted(c, gin.H{"run_id": runID})
}

// adHocRequest runs the given checks once on the given accounts, without a stored plan.
type adHocRequest struct {
	AccountIDs []int64       `json:"account_ids"`
	Checks     []CheckConfig `json:"checks"`
	Policy     Policy        `json:"policy"`
}

func (s *Service) runAdHoc(c *gin.Context) {
	var req adHocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body: "+err.Error())
		return
	}
	p := Plan{
		Name:       "ad-hoc",
		Checks:     req.Checks,
		ScopeType:  ScopeAccounts,
		AccountIDs: req.AccountIDs,
		Policy:     req.Policy,
	}
	if err := p.Normalize(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	runID, err := s.sched.RunAdHoc(c.Request.Context(), &p)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Accepted(c, gin.H{"run_id": runID})
}

// ---------- runs and results ----------

func (s *Service) listRuns(c *gin.Context) {
	planID := int64(-1)
	if v := c.Query("plan_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			response.BadRequest(c, "invalid plan_id")
			return
		}
		planID = n
	}
	page, size := response.ParsePagination(c)
	runs, total, err := s.store.ListRuns(c.Request.Context(), planID, size, (page-1)*size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Paginated(c, runs, int64(total), page, size)
}

func (s *Service) getRun(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	run, err := s.store.GetRun(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		response.NotFound(c, "run not found")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, run)
}

func (s *Service) listRunResults(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	page, size := response.ParsePagination(c)
	items, total, err := s.store.ListResultsByRun(c.Request.Context(), id, size, (page-1)*size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Paginated(c, items, int64(total), page, size)
}

func (s *Service) getResult(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	r, err := s.store.GetResult(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		response.NotFound(c, "result not found")
		return
	} else if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, r)
}

func (s *Service) listAccountResults(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	items, err := s.store.ListResultsByAccount(c.Request.Context(), id, c.Query("kind"), queryInt(c, "limit", 50, 200))
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, items)
}

// ---------- states and events ----------

func (s *Service) listStates(c *gin.Context) {
	page, size := response.ParsePagination(c)
	items, total, err := s.store.ListStates(c.Request.Context(), c.Query("flagged") == "true", c.Query("kind"), size, (page-1)*size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Paginated(c, items, int64(total), page, size)
}

// maxStateAccounts caps one by-accounts lookup; the account list pages are smaller.
const maxStateAccounts = 500

// listStatesByAccounts serves ?ids=1,2,3 with all check states of those accounts.
func (s *Service) listStatesByAccounts(c *gin.Context) {
	ids, err := parseIDList(c.Query("ids"), maxStateAccounts)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, err := s.store.ListStatesByAccounts(c.Request.Context(), ids)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, items)
}

func parseIDList(raw string, max int) ([]int64, error) {
	ids := []int64{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid id %q", part)
		}
		ids = append(ids, id)
	}
	if len(ids) > max {
		return nil, fmt.Errorf("at most %d ids", max)
	}
	return ids, nil
}

func (s *Service) listEvents(c *gin.Context) {
	page, size := response.ParsePagination(c)
	items, total, err := s.store.ListEvents(c.Request.Context(), size, (page-1)*size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Paginated(c, items, int64(total), page, size)
}

// clearAccountState resets the flag of one account and check by hand.
// It never changes the account's scheduling state.
func (s *Service) clearAccountState(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	kind := TestKind(c.Query("kind"))
	if kind != TestCandy && kind != TestPelicanSVG {
		response.BadRequest(c, "kind must be candy or pelican_svg")
		return
	}
	ctx := c.Request.Context()
	st, found, err := s.store.GetState(ctx, id, kind)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	if !found {
		response.NotFound(c, "no state for this account and kind")
		return
	}
	from := st.State
	st.State, st.ConsecutiveFails, st.ConsecutivePasses, st.FlaggedAt = StateNormal, 0, 0, nil
	st.Recent = nil
	st.UpdatedAt = time.Now()

	rows, _, err := s.store.ListStates(ctx, false, string(kind), 1000, 0)
	row := &StateRow{AccountID: id, Kind: kind, AccountState: st}
	if err == nil {
		for _, r := range rows {
			if r.AccountID == id {
				row.AccountName, row.Platform, row.PlanID = r.AccountName, r.Platform, r.PlanID
			}
		}
	}
	if err := s.store.UpsertState(ctx, row); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	if from != StateNormal {
		_ = s.store.InsertEvent(ctx, &Event{
			AccountID: id, AccountName: row.AccountName, Kind: kind, PlanID: row.PlanID,
			From: string(from), To: string(StateNormal), Detail: "cleared manually",
		})
	}
	response.Success(c, row)
}
