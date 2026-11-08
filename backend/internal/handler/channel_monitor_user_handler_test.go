package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type monitorMemberScopeStub struct {
	ids   map[int64]bool
	err   error
	users []int64
}

func (s *monitorMemberScopeStub) VisibleMonitorIDs(_ context.Context, userID int64) (map[int64]bool, error) {
	s.users = append(s.users, userID)
	return s.ids, s.err
}

type monitorUserReaderStub struct{ listCalls, detailCalls int }

func (s *monitorUserReaderStub) ListUserView(context.Context) ([]*service.UserMonitorView, error) {
	s.listCalls++
	return []*service.UserMonitorView{{ID: 1, Name: "My car"}, {ID: 2, Name: "Other car"}}, nil
}

func (s *monitorUserReaderStub) GetUserDetail(_ context.Context, id int64) (*service.UserMonitorDetail, error) {
	s.detailCalls++
	return &service.UserMonitorDetail{ID: id, Name: "Car details"}, nil
}

func TestChannelMonitorUserMembershipScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, role string
		userID     int64
		scope      *monitorMemberScopeStub
		wantList   []int64
		listCode   int
		detailCode int
	}{
		{"member", "user", 7, &monitorMemberScopeStub{ids: map[int64]bool{1: true}}, []int64{1}, 200, 404},
		{"other member", "user", 8, &monitorMemberScopeStub{ids: map[int64]bool{2: true}}, []int64{2}, 200, 200},
		{"no membership", "user", 9, &monitorMemberScopeStub{}, []int64{}, 200, 404},
		{"lookup failure", "user", 7, &monitorMemberScopeStub{err: errors.New("private database error")}, nil, 503, 503},
		{"missing lookup service", "user", 7, nil, nil, 503, 503},
		{"administrator", "admin", 1, nil, []int64{1, 2}, 200, 200},
		{"anonymous", "", 0, nil, nil, 401, 401},
		{"role alone cannot bypass authentication", "admin", 0, nil, nil, 401, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &monitorUserReaderStub{}
			h := &ChannelMonitorUserHandler{monitorService: reader}
			if tc.scope != nil {
				h.SetMemberVisibility(tc.scope)
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.userID != 0 {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.userID})
				}
				c.Set(string(middleware.ContextKeyUserRole), tc.role)
			})
			r.GET("/monitors", h.List)
			r.GET("/monitors/:id/status", h.GetStatus)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitors?user_id=1&group_id=2&role=admin", nil))
			require.Equal(t, tc.listCode, w.Code)
			require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "private database error")
			if tc.listCode == http.StatusOK {
				var body struct {
					Data struct {
						Items []channelMonitorUserListItem `json:"items"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				ids := []int64{}
				for _, item := range body.Data.Items {
					ids = append(ids, item.ID)
				}
				require.Equal(t, tc.wantList, ids)
			}
			if len(tc.wantList) == 0 {
				require.Zero(t, reader.listCalls, "empty/failed scopes must not load global monitoring data")
			}
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitors/2/status?user_id=1", nil))
			require.Equal(t, tc.detailCode, w.Code)
			if tc.detailCode != http.StatusOK {
				require.Zero(t, reader.detailCalls, "forbidden direct links must be rejected before reading details")
			}
			if tc.scope != nil {
				require.Equal(t, []int64{tc.userID, tc.userID}, tc.scope.users)
			}
		})
	}
}
