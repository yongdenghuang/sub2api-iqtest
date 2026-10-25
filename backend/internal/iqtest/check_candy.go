// Package iqtest implements scheduled per-account quality ("IQ") checks.
// Everything lives in this package so upstream upgrades rarely conflict.
package iqtest

import (
	"regexp"
	"strings"
)

// CandyAnswer is the expected answer of the candy puzzle.
const CandyAnswer = "21"

// CandyMode controls where the answer may appear.
type CandyMode string

const (
	// CandyModeAnywhere passes if a standalone 21 appears anywhere in the
	// visible reply (reasoning blocks stripped). This is the default.
	CandyModeAnywhere CandyMode = "anywhere"
	// CandyModeFinal only inspects the last non-empty paragraph, which
	// avoids false passes from numbers mentioned during derivation.
	CandyModeFinal CandyMode = "final"
)

// CandyResult is the outcome of a candy check.
type CandyResult struct {
	Passed  bool      `json:"passed"`
	Mode    CandyMode `json:"mode"`
	Excerpt string    `json:"excerpt"` // the text region that was inspected (truncated)
}

var thinkBlockRe = regexp.MustCompile(`(?is)<(think|thinking|reasoning)>.*?</(think|thinking|reasoning)>`)

// CheckCandy reports whether the reply contains the expected answer.
// An empty reply must be handled by the caller as an infrastructure error,
// not as a quality failure.
func CheckCandy(reply string, mode CandyMode) CandyResult {
	if mode != CandyModeFinal {
		mode = CandyModeAnywhere
	}
	text := stripReasoning(reply)
	region := text
	if mode == CandyModeFinal {
		region = lastParagraph(text)
	}
	return CandyResult{
		Passed:  containsStandalone(region, CandyAnswer),
		Mode:    mode,
		Excerpt: truncateRunes(strings.TrimSpace(region), 400),
	}
}

func stripReasoning(s string) string {
	s = thinkBlockRe.ReplaceAllString(s, "")
	// An unclosed <think> means the reply was cut off inside reasoning.
	if i := strings.LastIndex(strings.ToLower(s), "<think>"); i >= 0 {
		s = s[:i]
	}
	return s
}

func lastParagraph(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if i := strings.LastIndex(s, "\n\n"); i >= 0 {
		return strings.TrimSpace(s[i+2:])
	}
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// containsStandalone finds target as a stand-alone number: not part of a
// longer number (121, 2021), a decimal (21.5, 1.21), a thousands group
// (21,000), a percentage (21%), or an identifier (x21, 21st).
func containsStandalone(text, target string) bool {
	rs, t := []rune(text), []rune(target)
	for i := 0; i+len(t) <= len(rs); i++ {
		match := true
		for j := range t {
			if rs[i+j] != t[j] {
				match = false
				break
			}
		}
		if match && standaloneAt(rs, i, len(t)) {
			return true
		}
	}
	return false
}

func standaloneAt(rs []rune, start, n int) bool {
	end := start + n
	if start > 0 {
		p := rs[start-1]
		if isDigit(p) || isLetter(p) || p == '_' {
			return false
		}
		if (p == '.' || p == ',') && start >= 2 && isDigit(rs[start-2]) {
			return false
		}
	}
	if end < len(rs) {
		q := rs[end]
		if isDigit(q) || isLetter(q) || q == '_' || q == '%' {
			return false
		}
		if (q == '.' || q == ',') && end+1 < len(rs) && isDigit(rs[end+1]) {
			return false
		}
	}
	return true
}

func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
