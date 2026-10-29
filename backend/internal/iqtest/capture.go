package iqtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AccountTester is satisfied by *service.AccountTestService.
type AccountTester interface {
	TestAccountConnection(c *gin.Context, accountID int64, modelID string, prompt string, mode string, opts ...service.AccountTestOptions) error
}

// PinnedRequest asks one specific account to answer one prompt.
// The account is chosen by the caller; nothing here selects or switches accounts.
type PinnedRequest struct {
	AccountID    int64
	Model        string
	Prompt       string
	ImageDataURL string
	MaxTokens    int           // Claude test path only (needs the service hook); 0 keeps the official default
	Timeout      time.Duration // default 3 minutes
}

// Capture is everything observed while the account test ran.
type Capture struct {
	Events      []service.TestEvent // all events except "content"
	Text        string              // concatenated "content" text
	Complete    bool                // a test_complete event was seen
	Success     bool                // test_complete.Success
	ErrorMsg    string
	ErrorCode   string
	ErrorStatus string
	CallErr     error // error returned by TestAccountConnection (or a recovered panic)
	HTTPStatus  int
	Duration    time.Duration
	TimedOut    bool // our per-request timeout fired
	Cancelled   bool // the parent context was cancelled
}

const maxCapturedText = 1 << 20

// RunPinned calls the existing admin account-test pipeline for exactly one
// account against an in-memory response, then parses the SSE events it wrote.
func RunPinned(parent context.Context, tester AccountTester, req PinnedRequest) Capture {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/internal/iqtest", nil).WithContext(ctx)
	c.Set(service.AccountTestOverrideKey, service.AccountTestOverride{Prompt: req.Prompt, MaxTokens: req.MaxTokens, ImageDataURL: req.ImageDataURL})

	start := time.Now()
	var callErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				callErr = fmt.Errorf("panic in account test: %v", r)
			}
		}()
		callErr = tester.TestAccountConnection(c, req.AccountID, req.Model, req.Prompt, service.AccountTestModeDefault)
	}()

	out := parseSSE(rec.Body.String())
	out.CallErr = callErr
	out.Duration = time.Since(start)
	out.HTTPStatus = rec.Code
	out.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil
	out.Cancelled = errors.Is(parent.Err(), context.Canceled)
	return out
}

func parseSSE(body string) Capture {
	var out Capture
	var text strings.Builder
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev service.TestEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		if ev.Type == "content" {
			if text.Len() < maxCapturedText {
				_, _ = text.WriteString(ev.Text)
			}
		} else {
			out.Events = append(out.Events, ev)
		}
		if ev.Type == "test_complete" {
			out.Complete = true
			out.Success = ev.Success
		}
		if ev.Type == "error" || ev.Error != "" {
			out.ErrorMsg = firstNonEmpty(ev.Error, ev.Text)
			out.ErrorCode = ev.Code
			out.ErrorStatus = ev.Status
		}
	}
	out.Text = text.String()
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
