package iqtest

// TestKind identifies one quality check.
type TestKind string

const (
	TestCandy      TestKind = "candy"
	TestPelicanSVG TestKind = "pelican_svg"
)

// Outcome is the top-level result of one check on one account.
// Only pass and quality_fail feed the quality statistics.
type Outcome string

const (
	OutcomePass        Outcome = "pass"
	OutcomeQualityFail Outcome = "quality_fail"
	OutcomeInfraError  Outcome = "infra_error"
	OutcomeSkipped     Outcome = "skipped"
)

// CountsForQuality reports whether the outcome may influence anomaly flags.
func (o Outcome) CountsForQuality() bool {
	return o == OutcomePass || o == OutcomeQualityFail
}

// Reason refines an outcome.
type Reason string

const (
	ReasonNone Reason = ""

	// quality_fail
	ReasonAnswerMismatch       Reason = "answer_mismatch"
	ReasonSVGInvalid           Reason = "svg_invalid"
	ReasonVisualMismatch       Reason = "visual_mismatch"
	ReasonVisualReviewRequired Reason = "visual_review_required"

	// infra_error: never counted as a quality failure
	ReasonTimeout          Reason = "timeout"
	ReasonRateLimited      Reason = "rate_limited"
	ReasonQuotaExhausted   Reason = "quota_exhausted"
	ReasonAuthError        Reason = "auth_error"
	ReasonAuthExpired      Reason = "auth_expired"
	ReasonPermissionDenied Reason = "permission_denied"
	ReasonModelCatalog     Reason = "model_catalog_error"
	ReasonModelUnsupported Reason = "model_unsupported"
	ReasonUpstream5xx      Reason = "upstream_5xx"
	ReasonTruncated        Reason = "truncated"
	ReasonEmptyResponse    Reason = "empty_response"
	ReasonReviewError      Reason = "review_error"
	ReasonCancelled        Reason = "cancelled"
	ReasonUnknownError     Reason = "unknown_error"

	// skipped
	ReasonAccountDisabled     Reason = "account_disabled"
	ReasonConcurrencyFull     Reason = "concurrency_full"
	ReasonPlatformUnsupported Reason = "platform_unsupported"
	ReasonBudgetExhausted     Reason = "budget_exhausted"
)
