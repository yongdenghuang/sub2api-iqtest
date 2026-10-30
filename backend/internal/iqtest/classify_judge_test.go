package iqtest

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyErrorText(t *testing.T) {
	cases := []struct {
		msg     string
		outcome Outcome
		reason  Reason
	}{
		{`API returned 429: {"error":{"type":"rate_limit_error","message":"slow down"}}`, OutcomeInfraError, ReasonRateLimited},
		{`API returned 429: You exceeded your current quota, please check your plan and billing details`, OutcomeInfraError, ReasonQuotaExhausted},
		{`API returned 402: payment required`, OutcomeInfraError, ReasonQuotaExhausted},
		{`API returned 401: invalid x-api-key`, OutcomeInfraError, ReasonAuthError},
		{`API returned 401: model not supported`, OutcomeInfraError, ReasonAuthError},
		{`API returned 401: {"code":"token_revoked"}`, OutcomeInfraError, ReasonAuthExpired},
		{`token exchange failed: status 401, token_expired`, OutcomeInfraError, ReasonAuthExpired},
		{`API returned 403: forbidden`, OutcomeInfraError, ReasonPermissionDenied},
		{`API returned 403: Your credit balance is too low`, OutcomeInfraError, ReasonQuotaExhausted},
		{`API returned 404: model: claude-x not found`, OutcomeInfraError, ReasonModelUnsupported},
		{`API returned 400: invalid model name`, OutcomeInfraError, ReasonModelUnsupported},
		{`API returned 400: messages: text content blocks must be non-empty`, OutcomeInfraError, ReasonUnknownError},
		{`API returned 503: overloaded`, OutcomeInfraError, ReasonUpstream5xx},
		{`API returned 529: {"type":"overloaded_error"}`, OutcomeInfraError, ReasonUpstream5xx},
		{`API returned 504: gateway timeout`, OutcomeInfraError, ReasonTimeout},
		{`Request failed: Post "https://api.example.com/v1/messages": context deadline exceeded`, OutcomeInfraError, ReasonTimeout},
		{`Request failed: dial tcp 1.2.3.4:443: connect: connection refused`, OutcomeInfraError, ReasonNetworkError},
		{`No access token available`, OutcomeInfraError, ReasonAuthError},
		{`No API key available`, OutcomeInfraError, ReasonAuthError},
		{`Account not found`, OutcomeSkipped, ReasonAccountDisabled},
		{`something strange happened`, OutcomeInfraError, ReasonUnknownError},
	}
	for _, tc := range cases {
		got := classifyErrorText(tc.msg)
		if got.Outcome != tc.outcome || got.Reason != tc.reason {
			t.Errorf("%q => %s/%s, want %s/%s", tc.msg, got.Outcome, got.Reason, tc.outcome, tc.reason)
		}
	}
}

func TestClassifyCapture(t *testing.T) {
	cases := []struct {
		name    string
		c       Capture
		outcome Outcome
		reason  Reason
	}{
		{"usable", Capture{Complete: true, Success: true, Text: "x"}, "", ""},
		{"cancelled", Capture{Cancelled: true}, OutcomeInfraError, ReasonCancelled},
		{"timed out", Capture{TimedOut: true, Duration: 3 * time.Minute}, OutcomeInfraError, ReasonTimeout},
		{"error event", Capture{ErrorMsg: "API returned 429: slow down"}, OutcomeInfraError, ReasonRateLimited},
		{"call error only", Capture{CallErr: errors.New("API returned 503: busy")}, OutcomeInfraError, ReasonUpstream5xx},
		{"complete without success", Capture{Complete: true, Success: false}, OutcomeInfraError, ReasonUnknownError},
		{"empty after complete", Capture{Complete: true, Success: true, Text: "  "}, OutcomeInfraError, ReasonEmptyResponse},
		{"no complete no text", Capture{}, OutcomeInfraError, ReasonEmptyResponse},
		{"no complete with text", Capture{Text: "partial"}, OutcomeInfraError, ReasonTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyCapture(tc.c)
			if got.Outcome != tc.outcome || got.Reason != tc.reason {
				t.Fatalf("got %s/%s, want %s/%s", got.Outcome, got.Reason, tc.outcome, tc.reason)
			}
		})
	}
}

func okCapture(text string) Capture {
	return Capture{Complete: true, Success: true, Text: text, Duration: time.Second}
}

func TestJudgeCandy(t *testing.T) {
	cases := []struct {
		name    string
		c       Capture
		outcome Outcome
		reason  Reason
	}{
		{"pass", okCapture("答案是21"), OutcomePass, ""},
		{"mismatch", okCapture("答案是22"), OutcomeQualityFail, ReasonAnswerMismatch},
		{"only reasoning", okCapture("<think>也许21</think>"), OutcomeInfraError, ReasonTruncated},
		{"rate limited is not a quality failure", Capture{ErrorMsg: "API returned 429: slow down"}, OutcomeInfraError, ReasonRateLimited},
		{"timeout is not a quality failure", Capture{TimedOut: true}, OutcomeInfraError, ReasonTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JudgeCandy(tc.c, CandyModeAnywhere)
			if got.Outcome != tc.outcome || got.Reason != tc.reason {
				t.Fatalf("got %s/%s (%s), want %s/%s", got.Outcome, got.Reason, got.Detail, tc.outcome, tc.reason)
			}
			if got.Outcome.CountsForQuality() != (tc.outcome == OutcomePass || tc.outcome == OutcomeQualityFail) {
				t.Fatalf("CountsForQuality mismatch for %s", got.Outcome)
			}
		})
	}
}

func TestJudgeSVG(t *testing.T) {
	cases := []struct {
		name    string
		c       Capture
		outcome Outcome
		reason  Reason
	}{
		{"static only", okCapture("```svg\n" + goodSVG + "\n```"), OutcomeSkipped, ReasonVisualReviewRequired},
		{"no svg", okCapture("我画不出来"), OutcomeQualityFail, ReasonSVGInvalid},
		{"unsafe", okCapture(`<svg viewBox="0 0 1 1"><script>x()</script></svg>`), OutcomeQualityFail, ReasonSVGInvalid},
		{"unclosed is truncation", okCapture(`<svg viewBox="0 0 10 10">` + shapes(10)), OutcomeInfraError, ReasonTruncated},
		{"rate limited", Capture{ErrorMsg: "API returned 429: slow down"}, OutcomeInfraError, ReasonRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JudgeSVG(tc.c, SVGOptions{})
			if got.Outcome != tc.outcome || got.Reason != tc.reason {
				t.Fatalf("got %s/%s (%s), want %s/%s", got.Outcome, got.Reason, got.Detail, tc.outcome, tc.reason)
			}
		})
	}
	pass := JudgeSVG(okCapture(goodSVG), SVGOptions{})
	if pass.Score != 0 || pass.Output == "" {
		t.Fatalf("expected score and extracted svg, got score=%d len=%d", pass.Score, len(pass.Output))
	}
}
