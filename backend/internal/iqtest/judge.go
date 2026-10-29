package iqtest

import "strings"

// Result is the final verdict for one check on one account.
type Result struct {
	Kind       TestKind `json:"kind"`
	Outcome    Outcome  `json:"outcome"`
	Reason     Reason   `json:"reason,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Output     string   `json:"output,omitempty"` // answer excerpt (candy) or extracted SVG
	Score      int      `json:"score,omitempty"`  // visual rubric score; never the old structure heuristic
	Metrics    any      `json:"metrics,omitempty"`
	Requests   int      `json:"-"` // generation and review upstream requests
	DurationMs int64    `json:"duration_ms"`
}

func (r *Result) apply(c Classification) {
	r.Outcome, r.Reason, r.Detail = c.Outcome, c.Reason, c.Detail
}

// JudgeCandy turns a captured run into a verdict for the candy puzzle.
func JudgeCandy(c Capture, mode CandyMode) Result {
	res := Result{Kind: TestCandy, DurationMs: c.Duration.Milliseconds()}
	if cls := ClassifyCapture(c); cls.Outcome != "" {
		res.apply(cls)
		return res
	}
	if strings.TrimSpace(stripReasoning(c.Text)) == "" {
		res.apply(Classification{
			Outcome: OutcomeInfraError,
			Reason:  ReasonTruncated,
			Detail:  "only reasoning text, no visible answer",
		})
		return res
	}
	r := CheckCandy(c.Text, mode)
	res.Output = r.Excerpt
	if r.Passed {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome, res.Reason, res.Detail = OutcomeQualityFail, ReasonAnswerMismatch, "standalone 21 not found"
	return res
}

// JudgeSVG turns a captured run into a verdict for the pelican SVG test.
// An unterminated SVG is treated as truncation (infra), because the account
// test path may cap output tokens; it never counts as a quality failure.
func JudgeSVG(c Capture, opts SVGOptions) Result {
	res := Result{Kind: TestPelicanSVG, DurationMs: c.Duration.Milliseconds()}
	if cls := ClassifyCapture(c); cls.Outcome != "" {
		res.apply(cls)
		return res
	}
	visible := strings.TrimSpace(stripReasoning(c.Text))
	if visible == "" {
		res.apply(Classification{
			Outcome: OutcomeInfraError,
			Reason:  ReasonTruncated,
			Detail:  "only reasoning text, no visible answer",
		})
		return res
	}

	opts.MinScore = 0  // Structural complexity is not a visual-quality gate.
	opts.MinShapes = 1 // Even one complex path can be a complete drawing; vision decides.
	r := CheckSVG(c.Text, opts)
	res.Metrics = SVGReviewMetrics{EvaluationVersion: VisualRubricVersion, Static: r.Metrics, Requests: 1}
	res.Output = r.SVG
	if res.Output == "" {
		res.Output = truncateRunes(visible, 2000)
	}
	switch {
	case r.Passed:
		res.Outcome, res.Reason = OutcomeSkipped, ReasonVisualReviewRequired
		res.Detail = "SVG structure checked; visual review is required before a quality verdict"
	case r.Failure == SVGFailUnclosed:
		res.Outcome, res.Reason = OutcomeInfraError, ReasonTruncated
		res.Detail = "svg not closed; reply was probably cut off by the token limit"
	default:
		res.Outcome, res.Reason = OutcomeQualityFail, ReasonSVGInvalid
		res.Detail = string(r.Failure) + ": " + r.Detail
	}
	return res
}
