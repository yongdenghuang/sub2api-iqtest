package iqtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func init() { gin.SetMode(gin.TestMode) }

type fakeTester struct {
	fn func(c *gin.Context, id int64, model, prompt string) error
}

func (f fakeTester) TestAccountConnection(c *gin.Context, id int64, model, prompt, _ string, _ ...service.AccountTestOptions) error {
	return f.fn(c, id, model, prompt)
}

func emit(c *gin.Context, ev service.TestEvent) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(c.Writer, "data: %s\n\n", b)
	c.Writer.Flush()
}

func TestRunPinnedSuccess(t *testing.T) {
	var gotID int64
	var gotModel, gotPrompt string
	f := fakeTester{fn: func(c *gin.Context, id int64, model, prompt string) error {
		gotID, gotModel, gotPrompt = id, model, prompt
		emit(c, service.TestEvent{Type: "test_start", Model: model})
		emit(c, service.TestEvent{Type: "content", Text: "答案"})
		emit(c, service.TestEvent{Type: "content", Text: "是21"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}
	out := RunPinned(context.Background(), f, PinnedRequest{AccountID: 42, Model: "m", Prompt: "p"})
	if gotID != 42 || gotModel != "m" || gotPrompt != "p" {
		t.Fatalf("args not forwarded: %d %q %q", gotID, gotModel, gotPrompt)
	}
	if out.Text != "答案是21" || !out.Complete || !out.Success || out.CallErr != nil {
		t.Fatalf("unexpected capture: %+v", out)
	}
	if len(out.Events) != 2 {
		t.Fatalf("events = %d, want 2 (start + complete)", len(out.Events))
	}
}

func TestRunPinnedError(t *testing.T) {
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "error", Error: "upstream 429"})
		return errors.New("failed")
	}}
	out := RunPinned(context.Background(), f, PinnedRequest{AccountID: 1})
	if out.ErrorMsg != "upstream 429" || out.CallErr == nil || out.Complete {
		t.Fatalf("unexpected capture: %+v", out)
	}
}

func TestRunPinnedRecoversPanic(t *testing.T) {
	f := fakeTester{fn: func(_ *gin.Context, _ int64, _, _ string) error { panic("boom") }}
	out := RunPinned(context.Background(), f, PinnedRequest{AccountID: 1})
	if out.CallErr == nil {
		t.Fatalf("panic should surface as CallErr")
	}
}

func TestRunPinnedTimeout(t *testing.T) {
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		<-c.Request.Context().Done()
		return c.Request.Context().Err()
	}}
	out := RunPinned(context.Background(), f, PinnedRequest{AccountID: 1, Timeout: 50 * time.Millisecond})
	if !out.TimedOut || out.Cancelled {
		t.Fatalf("expected TimedOut only: %+v", out)
	}
}
