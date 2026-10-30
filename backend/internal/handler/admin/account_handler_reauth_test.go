package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type reauthAdminStub struct {
	*stubAdminService
	account *service.Account
	fail    string
	steps   []string
}

func (s *reauthAdminStub) GetAccount(context.Context, int64) (*service.Account, error) {
	a := *s.account
	return &a, nil
}

func (s *reauthAdminStub) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.steps = append(s.steps, "save")
	s.account.Credentials = input.Credentials
	s.account.Type = input.Type
	return s.GetAccount(context.Background(), s.account.ID)
}

func (s *reauthAdminStub) UpdateAccountExtra(_ context.Context, _ int64, extra map[string]any) error {
	s.steps = append(s.steps, "extra")
	if s.fail == "extra" {
		return errors.New("metadata unavailable")
	}
	for k, v := range extra {
		s.account.Extra[k] = v
	}
	return nil
}

func (s *reauthAdminStub) ClearAccountError(context.Context, int64) (*service.Account, error) {
	s.steps = append(s.steps, "clear")
	if s.fail == "clear" {
		return nil, errors.New("cleanup unavailable")
	}
	s.account.Status = service.StatusActive
	s.account.ErrorMessage = ""
	s.account.TempUnschedulableUntil = nil
	s.account.RateLimitResetAt = nil
	return s.GetAccount(context.Background(), s.account.ID)
}

func (s *reauthAdminStub) SetAccountSchedulable(_ context.Context, _ int64, enabled bool) (*service.Account, error) {
	s.steps = append(s.steps, "schedule")
	if s.fail == "schedule" {
		return nil, errors.New("scheduling unavailable")
	}
	s.account.Schedulable = enabled
	return s.GetAccount(context.Background(), s.account.ID)
}

type reauthInvalidatorStub struct{ service *reauthAdminStub }

func (s reauthInvalidatorStub) InvalidateToken(_ context.Context, account *service.Account) error {
	s.service.steps = append(s.service.steps, "invalidate")
	if s.service.fail == "invalidate" {
		return errors.New("redis unavailable")
	}
	return nil
}

func newReauthFixture(status string) (*reauthAdminStub, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	until := time.Now().Add(time.Hour)
	s := &reauthAdminStub{stubAdminService: newStubAdminService(), account: &service.Account{
		ID: 6, Name: "retained account", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: status, Schedulable: false, ErrorMessage: "token_revoked", Priority: 17,
		RateLimitResetAt: &until, TempUnschedulableUntil: &until,
		Credentials: map[string]any{"access_token": "old-secret", "refresh_token": "old-refresh",
			"_token_version": time.Now().Add(time.Hour).UnixMilli(), "model_mapping": map[string]any{"alias": "gpt-6"}},
		Extra: map[string]any{"base_rpm": 15, "max_sessions": 3},
	}}
	h := NewAccountHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, reauthInvalidatorStub{s})
	r := gin.New()
	r.POST("/accounts/:id/apply-oauth-credentials", h.ApplyOAuthCredentials)
	return s, r
}

func applyReauth(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/accounts/6/apply-oauth-credentials", bytes.NewBufferString(
		`{"type":"oauth","credentials":{"access_token":"new-secret","refresh_token":"new-refresh"},"extra":{"privacy_mode":"training_off"}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestApplyOAuthCredentialsRecoversErroredAccountInPlace(t *testing.T) {
	s, r := newReauthFixture(service.StatusError)
	previous := s.account.GetCredentialAsInt64("_token_version")
	w := applyReauth(r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"save", "invalidate", "extra", "schedule", "clear"}, s.steps)
	require.True(t, s.account.Schedulable)
	require.Equal(t, service.StatusActive, s.account.Status)
	require.Empty(t, s.account.ErrorMessage)
	require.Nil(t, s.account.RateLimitResetAt)
	require.Nil(t, s.account.TempUnschedulableUntil)
	require.Greater(t, s.account.GetCredentialAsInt64("_token_version"), previous)
	require.Equal(t, "new-secret", s.account.GetCredential("access_token"))
	require.Equal(t, map[string]any{"alias": "gpt-6"}, s.account.Credentials["model_mapping"])
	require.Equal(t, 15, s.account.Extra["base_rpm"])
	require.Equal(t, 17, s.account.Priority)
	require.Equal(t, int64(6), s.account.ID)
	require.NotContains(t, w.Body.String(), "new-secret")
	require.NotContains(t, w.Body.String(), "new-refresh")
}

func TestApplyOAuthCredentialsPreservesManualPause(t *testing.T) {
	for _, status := range []string{service.StatusActive, service.StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			s, r := newReauthFixture(status)
			require.Equal(t, http.StatusOK, applyReauth(r).Code)
			require.Equal(t, status, s.account.Status)
			require.False(t, s.account.Schedulable)
			require.NotContains(t, s.steps, "schedule")
		})
	}
}

func TestApplyOAuthCredentialsReportsRecoveryFailureAndAllowsRetry(t *testing.T) {
	for _, step := range []string{"extra", "invalidate", "schedule", "clear"} {
		t.Run(step, func(t *testing.T) {
			s, r := newReauthFixture(service.StatusError)
			s.fail = step
			w := applyReauth(r)
			require.GreaterOrEqual(t, w.Code, 500)
			require.Equal(t, "new-secret", s.account.GetCredential("access_token"))
			require.Equal(t, service.StatusError, s.account.Status)
			s.fail = ""
			require.Equal(t, http.StatusOK, applyReauth(r).Code)
			require.True(t, s.account.Schedulable)
			require.Equal(t, service.StatusActive, s.account.Status)
		})
	}
}
