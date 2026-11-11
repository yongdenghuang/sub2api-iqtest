package carpool

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (s *Service) RegisterMemberRoutes(v1 *gin.RouterGroup, mw ...gin.HandlerFunc) {
	g := v1.Group("/carpool", mw...)
	g.GET("", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "Authentication required")
			return
		}
		rows, err := s.ListForMember(c.Request.Context(), subject.UserID)
		if err != nil {
			response.InternalError(c, "Unable to load shared quota")
			return
		}
		response.Success(c, rows)
	})
}

func (s *Service) RegisterRoutes(v1 *gin.RouterGroup, mw ...gin.HandlerFunc) {
	g := v1.Group("/admin/carpool", mw...)
	g.GET("/groups", func(c *gin.Context) {
		rows, err := s.List(c.Request.Context())
		if err != nil {
			response.InternalError(c, err.Error())
			return
		}
		response.Success(c, rows)
	})
	g.PUT("/groups/:id", func(c *gin.Context) {
		id, ok := groupID(c)
		if !ok {
			return
		}
		var cfg SaveRequest
		if err := c.ShouldBindJSON(&cfg); err != nil {
			response.BadRequest(c, "配置格式不正确")
			return
		}
		if err := s.save(c.Request.Context(), id, cfg); err != nil {
			respondSaveError(c, err)
			return
		}
		response.Success(c, gin.H{"saved": true})
	})
	g.POST("/groups/:id/sync", func(c *gin.Context) {
		id, ok := groupID(c)
		if !ok {
			return
		}
		if err := s.Sync(c.Request.Context(), id); err != nil {
			respondSaveError(c, err)
			return
		}
		g, err := loadGroup(c.Request.Context(), s.db, id)
		if err != nil {
			response.InternalError(c, err.Error())
			return
		}
		response.Success(c, g)
	})
	g.GET("/groups/:id/events", func(c *gin.Context) {
		id, ok := groupID(c)
		if !ok {
			return
		}
		rows, err := s.db.QueryContext(c.Request.Context(), "SELECT id,kind,detail,created_at FROM carpool_events WHERE group_id=$1 ORDER BY id DESC LIMIT 30", id)
		if err != nil {
			response.InternalError(c, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()
		events := []gin.H{}
		for rows.Next() {
			var id int64
			var kind string
			var detail json.RawMessage
			var at time.Time
			if err = rows.Scan(&id, &kind, &detail, &at); err != nil {
				response.InternalError(c, err.Error())
				return
			}
			events = append(events, gin.H{"id": id, "kind": kind, "detail": detail, "created_at": at})
		}
		if err = rows.Err(); err != nil {
			response.InternalError(c, err.Error())
			return
		}
		response.Success(c, events)
	})
}

func respondSaveError(c *gin.Context, err error) {
	var conflict *BindingConflict
	if errors.As(err, &conflict) {
		response.ErrorWithDetails(c, http.StatusConflict, err.Error(), "CARPOOL_BINDING_CHANGED", nil)
		return
	}
	response.BadRequest(c, err.Error())
}

func groupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 || id > 2147483647 {
		response.BadRequest(c, "invalid group id")
		return 0, false
	}
	return id, true
}
