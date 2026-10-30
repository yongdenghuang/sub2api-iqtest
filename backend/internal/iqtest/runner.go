package iqtest

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReasonCoolingDown marks accounts skipped because they are currently
// rate limited, overloaded or temporarily unschedulable.
const ReasonCoolingDown Reason = "cooling_down"

// DefaultModel is the legacy fallback for direct runner/non-OpenAI callers.
// OpenAI plans and ad-hoc runs resolve AutoModel through the live catalog.
const DefaultModel = openai.DefaultTestModel

// CheckConfig describes one check inside a plan. Zero values select defaults.
type CheckConfig struct {
	Kind           TestKind   `json:"kind"`
	Prompt         string     `json:"prompt,omitempty"`          // empty = built-in prompt
	Model          string     `json:"model,omitempty"`           // empty/auto = strongest available per account (engine)
	MaxTokens      int        `json:"max_tokens,omitempty"`      // Claude test path only
	TimeoutSeconds int        `json:"timeout_seconds,omitempty"` // per request
	CandyMode      CandyMode  `json:"candy_mode,omitempty"`
	SVG            SVGOptions `json:"svg,omitempty"`
}

func (c CheckConfig) withDefaults() CheckConfig {
	c.Model = strings.TrimSpace(c.Model)
	if c.Model == "" {
		c.Model = DefaultModel
	}
	switch c.Kind {
	case TestCandy:
		if c.Prompt == "" {
			c.Prompt = DefaultCandyPrompt
		}
		if c.MaxTokens <= 0 {
			c.MaxTokens = 4096
		}
		if c.TimeoutSeconds <= 0 {
			c.TimeoutSeconds = 180
		}
		if c.CandyMode == "" {
			c.CandyMode = CandyModeAnywhere
		}
	case TestPelicanSVG:
		if c.Prompt == "" {
			c.Prompt = DefaultPelicanPrompt
		}
		if c.MaxTokens <= 0 {
			c.MaxTokens = 8192
		}
		if c.TimeoutSeconds <= 0 {
			c.TimeoutSeconds = 300
		}
	}
	return c
}

// RunCheck asks exactly one account to answer one check and returns the verdict.
// It never selects or switches accounts, and it never retries on another one.
func RunCheck(ctx context.Context, tester AccountTester, accountID int64, cfg CheckConfig) Result {
	cfg = cfg.withDefaults()
	out := RunPinned(ctx, tester, PinnedRequest{
		AccountID: accountID,
		Model:     cfg.Model,
		Prompt:    cfg.Prompt,
		MaxTokens: cfg.MaxTokens,
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
	})
	switch cfg.Kind {
	case TestCandy:
		r := JudgeCandy(out, cfg.CandyMode)
		r.Requests = 1
		r.Model = firstNonEmpty(out.Model, cfg.Model)
		return r
	case TestPelicanSVG:
		r := JudgeSVG(out, cfg.SVG)
		r.Requests = 1
		r.Model = firstNonEmpty(out.Model, cfg.Model)
		return r
	}
	return Result{
		Kind:    cfg.Kind,
		Outcome: OutcomeSkipped,
		Reason:  ReasonPlatformUnsupported,
		Detail:  "unknown check kind",
	}
}

// Eligibility says whether an account can be tested right now.
type Eligibility struct {
	OK     bool
	Reason Reason
	Detail string
}

// CheckEligibility decides before any request is sent. Accounts that cannot
// carry a custom prompt, or are cooling down, are skipped (and recorded as such)
// rather than failed.
func CheckEligibility(a *service.Account, now time.Time) Eligibility {
	skip := func(r Reason, d string) Eligibility { return Eligibility{Reason: r, Detail: d} }
	switch {
	case a == nil:
		return skip(ReasonAccountDisabled, "account not found")
	case a.IsSyntheticUITest():
		return skip(ReasonPlatformUnsupported, "synthetic UI test account")
	case a.Status == service.StatusError:
		cls := classifyErrorText(a.ErrorMessage)
		return skip(cls.Reason, a.ErrorMessage)
	case !a.IsActive():
		return skip(ReasonAccountDisabled, "account is not active")
	case a.IsBedrock() || a.Type == service.AccountTypeServiceAccount:
		return skip(ReasonPlatformUnsupported, "Bedrock/Vertex test path cannot carry a custom prompt")
	case a.RoutesProtocolByInbound() && a.GetAPIProtocol() == service.APIProtocolAnthropic:
		return skip(ReasonPlatformUnsupported, "Anthropic-protocol test path cannot carry a custom prompt")
	case isFuture(a.RateLimitResetAt, now), isFuture(a.OverloadUntil, now), isFuture(a.TempUnschedulableUntil, now):
		return skip(ReasonCoolingDown, "rate limited, overloaded or temporarily unschedulable")
	}
	return Eligibility{OK: true}
}

func isFuture(t *time.Time, now time.Time) bool { return t != nil && t.After(now) }
