package iqtest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReasonNetworkError marks transport failures that are not timeouts.
const ReasonNetworkError Reason = "network_error"

// Classification is a transport-level verdict. A zero Outcome means the
// capture is usable and the content can be judged.
type Classification struct {
	Outcome Outcome
	Reason  Reason
	Detail  string
}

// Error texts produced by AccountTestService look like
//
//	API returned <status>: <body>      (upstream answered non-200)
//	Request failed: <error>            (transport error)
//	No access token available / No API key available / Account not found
var apiStatusRe = regexp.MustCompile(`(?i)(?:API returned |status[\s:=]+)(\d{3})\b`)

// ClassifyCapture decides whether the run produced something we may judge.
// Anything ambiguous is classified as infra_error so it can never be counted
// as a quality failure.
func ClassifyCapture(c Capture) Classification {
	switch {
	case c.Cancelled:
		return Classification{Outcome: OutcomeInfraError, Reason: ReasonCancelled, Detail: "run cancelled"}
	case c.TimedOut:
		return Classification{
			Outcome: OutcomeInfraError,
			Reason:  ReasonTimeout,
			Detail:  fmt.Sprintf("no answer within %s", c.Duration.Round(time.Second)),
		}
	}

	msg := c.ErrorMsg
	if msg == "" && c.CallErr != nil {
		msg = c.CallErr.Error()
	}
	if msg != "" || (c.Complete && !c.Success) {
		if msg == "" {
			msg = "test finished without success"
		}
		return classifyErrorText(msg)
	}

	empty := strings.TrimSpace(c.Text) == ""
	if !c.Complete {
		if empty {
			return Classification{Outcome: OutcomeInfraError, Reason: ReasonEmptyResponse, Detail: "no content and no completion event"}
		}
		return Classification{Outcome: OutcomeInfraError, Reason: ReasonTruncated, Detail: "stream ended before completion"}
	}
	if empty {
		return Classification{Outcome: OutcomeInfraError, Reason: ReasonEmptyResponse, Detail: "completed with empty content"}
	}
	return Classification{}
}

func classifyErrorText(msg string) Classification {
	lower := strings.ToLower(msg)
	detail := truncateRunes(msg, 300)
	infra := func(r Reason) Classification {
		return Classification{Outcome: OutcomeInfraError, Reason: r, Detail: detail}
	}

	if containsAny(lower, "token_revoked", "token_expired", "refresh_token_reused", "invalid_grant", "refresh token expired", "refresh token has expired") {
		return infra(ReasonAuthExpired)
	}
	if m := apiStatusRe.FindStringSubmatch(msg); m != nil {
		status, _ := strconv.Atoi(m[1])
		quota := mentionsQuota(lower)
		switch {
		case status == 402 || ((status == 429 || status == 403) && quota):
			return infra(ReasonQuotaExhausted)
		case status == 429:
			return infra(ReasonRateLimited)
		case status == 401:
			return infra(ReasonAuthError)
		case status == 403:
			return infra(ReasonPermissionDenied)
		case status == 408 || status == 504:
			return infra(ReasonTimeout)
		case status == 404 || (status == 400 && mentionsModelProblem(lower)):
			return infra(ReasonModelUnsupported)
		case status >= 500:
			return infra(ReasonUpstream5xx)
		}
		return infra(ReasonUnknownError)
	}

	switch {
	case containsAny(lower, "model_not_found") || mentionsModelProblem(lower):
		return infra(ReasonModelUnsupported)
	case strings.HasPrefix(lower, "no access token") || strings.HasPrefix(lower, "no api key"):
		return infra(ReasonAuthError)
	case strings.HasPrefix(lower, "account not found"):
		return Classification{Outcome: OutcomeSkipped, Reason: ReasonAccountDisabled, Detail: detail}
	case containsAny(lower, "deadline exceeded", "timeout", "timed out"):
		return infra(ReasonTimeout)
	case containsAny(lower, "rate limit", "ratelimit", "too many requests"):
		return infra(ReasonRateLimited)
	case mentionsQuota(lower):
		return infra(ReasonQuotaExhausted)
	case strings.HasPrefix(lower, "request failed"):
		return infra(ReasonNetworkError)
	}
	return infra(ReasonUnknownError)
}

func mentionsQuota(lower string) bool {
	return containsAny(lower,
		"insufficient", "quota", "billing", "credit balance", "out of credits",
		"usage limit", "exceeded your current", "balance", "余额", "额度", "欠费")
}

func mentionsModelProblem(lower string) bool {
	return strings.Contains(lower, "model") &&
		containsAny(lower, "not found", "not supported", "does not exist", "invalid", "unsupported", "unknown", "not available")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
