package iqtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestApplyAccountTestOverride(t *testing.T) {
	payload := map[string]any{
		"max_tokens": 1024,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{{"type": "text", "text": "hi"}}},
		},
	}
	text := func() any {
		messages, ok := payload["messages"].([]map[string]any)
		require.True(t, ok)
		require.NotEmpty(t, messages)
		content, ok := messages[0]["content"].([]map[string]any)
		require.True(t, ok)
		require.NotEmpty(t, content)
		return content[0]["text"]
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	service.ApplyAccountTestOverride(c, payload)
	if payload["max_tokens"] != 1024 || text() != "hi" {
		t.Fatalf("payload must be untouched without an override: %v", payload)
	}

	c.Set(service.AccountTestOverrideKey, service.AccountTestOverride{Prompt: "question", MaxTokens: 4096})
	service.ApplyAccountTestOverride(c, payload)
	if payload["max_tokens"] != 4096 || text() != "question" {
		t.Fatalf("override not applied: %v", payload)
	}
}

func TestRunCheckCandy(t *testing.T) {
	var gotPrompt string
	var gotOverride service.AccountTestOverride
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, prompt string) error {
		gotPrompt = prompt
		if v, ok := c.Get(service.AccountTestOverrideKey); ok {
			gotOverride, _ = v.(service.AccountTestOverride)
		}
		emit(c, service.TestEvent{Type: "content", Text: "最少需要取出 21 颗。"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}
	res := RunCheck(context.Background(), f, 7, CheckConfig{Kind: TestCandy})
	if res.Outcome != OutcomePass {
		t.Fatalf("want pass, got %s/%s (%s)", res.Outcome, res.Reason, res.Detail)
	}
	if !strings.Contains(gotPrompt, "糖果") || gotOverride.Prompt != gotPrompt || gotOverride.MaxTokens != 4096 {
		t.Fatalf("default prompt/override not passed: %q %+v", gotPrompt, gotOverride)
	}
}

func TestRunCheckCandyWrongAnswer(t *testing.T) {
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "content", Text: "答案是 29"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}
	res := RunCheck(context.Background(), f, 7, CheckConfig{Kind: TestCandy})
	if res.Outcome != OutcomeQualityFail || res.Reason != ReasonAnswerMismatch {
		t.Fatalf("want quality_fail, got %s/%s", res.Outcome, res.Reason)
	}
}

func TestRunCheckPelican(t *testing.T) {
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "content", Text: "```svg\n" + goodSVG + "\n```"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}
	res := RunCheck(context.Background(), f, 7, CheckConfig{Kind: TestPelicanSVG})
	if res.Outcome != OutcomePass || res.Score < 60 {
		t.Fatalf("want pass with score, got %s/%s score=%d", res.Outcome, res.Reason, res.Score)
	}
}

func TestRunCheckUpstreamErrorIsInfra(t *testing.T) {
	f := fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "error", Error: "API returned 429: rate limited"})
		return http.ErrAbortHandler
	}}
	res := RunCheck(context.Background(), f, 7, CheckConfig{Kind: TestCandy})
	if res.Outcome != OutcomeInfraError || res.Reason != ReasonRateLimited || res.Outcome.CountsForQuality() {
		t.Fatalf("429 must be an infra error: %s/%s", res.Outcome, res.Reason)
	}
}

func TestCheckEligibility(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	if e := CheckEligibility(nil, now); e.OK || e.Reason != ReasonAccountDisabled {
		t.Fatalf("nil account: %+v", e)
	}
	ok := &service.Account{Platform: "anthropic", Type: "apikey", Status: "active", RateLimitResetAt: &past}
	if e := CheckEligibility(ok, now); !e.OK {
		t.Fatalf("active account with an expired limit must be eligible: %+v", e)
	}
	cooling := &service.Account{Platform: "anthropic", Type: "apikey", Status: "active", RateLimitResetAt: &future}
	if e := CheckEligibility(cooling, now); e.OK || e.Reason != ReasonCoolingDown {
		t.Fatalf("cooling account: %+v", e)
	}
	vertex := &service.Account{Platform: "anthropic", Type: service.AccountTypeServiceAccount, Status: "active"}
	if e := CheckEligibility(vertex, now); e.OK || e.Reason != ReasonPlatformUnsupported {
		t.Fatalf("service account: %+v", e)
	}
}

func TestRunCheckDefaultModel(t *testing.T) {
	for _, kind := range []TestKind{TestCandy, TestPelicanSVG} {
		for _, tc := range []struct{ model, want string }{
			{"", "gpt-6-astra"}, {"  ", "gpt-6-astra"}, {" custom-model ", "custom-model"},
		} {
			t.Run(string(kind)+"/"+tc.model, func(t *testing.T) {
				var got string
				tester := fakeTester{fn: func(c *gin.Context, _ int64, model, _ string) error {
					got = model
					emit(c, service.TestEvent{Type: "content", Text: "21"})
					emit(c, service.TestEvent{Type: "test_complete", Success: true})
					return nil
				}}
				RunCheck(context.Background(), tester, 7, CheckConfig{Kind: kind, Model: tc.model})
				if got != tc.want {
					t.Fatalf("model=%q want=%q", got, tc.want)
				}
			})
		}
	}
}
