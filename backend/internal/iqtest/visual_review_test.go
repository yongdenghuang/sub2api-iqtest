package iqtest

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const validVisualJSON = `{"pelican":{"score":23,"reason":"长喙和喉囊清楚，头颈连接完整"},"bicycle":{"score":23,"reason":"双轮、车架、车把、脚踏可辨"},"riding":{"score":26,"reason":"鸟坐车座，脚接触脚踏，翅膀扶车把"},"anatomy":{"score":9,"reason":"轮廓连贯"},"clarity":{"score":9,"reason":"主体完整"},"critical_errors":[],"confidence":"high","summary":"结构与骑乘关系清楚"}`

func TestVisualRubricGates(t *testing.T) {
	v, score, pass, err := decodeVisualAssessment(validVisualJSON)
	require.NoError(t, err)
	require.True(t, pass)
	require.Equal(t, 90, score)
	require.NotNil(t, v)
	for _, tc := range []struct {
		name, reply string
		invalid     bool
	}{
		{"malformed", "pass: 100", true},
		{"no evidence", strings.Replace(validVisualJSON, `"reason":"轮廓连贯"`, `"reason":""`, 1), true},
		{"missing score", strings.Replace(validVisualJSON, `"score":9,`, "", 1), true},
		{"out of range", strings.Replace(validVisualJSON, `"score":23`, `"score":100`, 1), true},
		{"low confidence", strings.Replace(validVisualJSON, `"high"`, `"low"`, 1), true},
		{"missing defects field", strings.Replace(validVisualJSON, `"critical_errors":[],`, "", 1), true},
		{"critical defect overrides high score", strings.Replace(validVisualJSON, `"critical_errors":[]`, `"critical_errors":["major_structure_error"]`, 1), false},
		{"core minimum overrides total", strings.Replace(validVisualJSON, `"score":26`, `"score":17`, 1), false},
		{"total below threshold", strings.ReplaceAll(validVisualJSON, `"score":23`, `"score":15`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, passed, err := decodeVisualAssessment(tc.reply)
			require.False(t, passed)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEngineVisualReviewUsesPixelsAndDifferentAccount(t *testing.T) {
	repo := newMemRepo()
	a, b := acct(1), acct(2)
	a.Platform, b.Platform = service.PlatformOpenAI, service.PlatformOpenAI
	raw, err := os.ReadFile("testdata/broken_pelican.svg")
	require.NoError(t, err)
	// This real regression image reached 100 in the old structural heuristic.
	structural := CheckSVG(string(raw), SVGOptions{})
	require.True(t, structural.Passed)
	require.Equal(t, 100, structural.Score)
	calls := []int64{}
	e := newTestEngine(repo, []*service.Account{a, b}, fakeTester{fn: func(c *gin.Context, id int64, model, prompt string) error {
		calls = append(calls, id)
		ov, ok := c.Get(service.AccountTestOverrideKey)
		require.True(t, ok)
		override, ok := ov.(service.AccountTestOverride)
		require.True(t, ok)
		text := string(raw)
		if id == 2 {
			require.Equal(t, DefaultModel, model)
			require.Contains(t, prompt, "major_structure_error")
			require.True(t, strings.HasPrefix(override.ImageDataURL, "data:image/png;base64,"))
			pixels, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(override.ImageDataURL, "data:image/png;base64,"))
			require.NoError(t, err)
			require.Equal(t, "\x89PNG\r\n\x1a\n", string(pixels[:8]))
			text = strings.Replace(validVisualJSON, `"critical_errors":[]`, `"critical_errors":["major_structure_error"]`, 1)
		} else {
			require.Empty(t, override.ImageDataURL)
		}
		emit(c, service.TestEvent{Type: "content", Text: text})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}})
	p := newPlan()
	p.Checks = []CheckConfig{{Kind: TestPelicanSVG}}
	p.ScopeType = ScopeAccounts
	p.AccountIDs = []int64{1}
	stats := runOnce(t, e, p)
	require.Equal(t, []int64{1, 2}, calls)
	require.Equal(t, 2, stats.Requests)
	require.Equal(t, 1, stats.QualityFail)
	require.Zero(t, stats.Pass)
	require.Equal(t, ReasonVisualMismatch, repo.results[0].Reason)
	metrics, ok := repo.results[0].Metrics.(SVGReviewMetrics)
	require.True(t, ok)
	require.Equal(t, "completed", metrics.Visual.Status)
	require.NotEmpty(t, metrics.Visual.ImageSHA256)
}

func TestVisualReviewFailureIsNotPassOrQualityFailure(t *testing.T) {
	for _, scenario := range []string{"missing reviewer", "review request fails", "invalid review", "render fails", "budget exhausted", "reviewer degraded"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newMemRepo()
			a, b := acct(1), acct(2)
			a.Platform, b.Platform = service.PlatformOpenAI, service.PlatformOpenAI
			accounts := []*service.Account{a, b}
			if scenario == "missing reviewer" {
				accounts = accounts[:1]
			}
			if scenario == "reviewer degraded" {
				repo.states[skey(2, TestCandy)] = &StateRow{AccountState: AccountState{LastOutcome: OutcomeQualityFail}}
			}
			calls := 0
			e := newTestEngine(repo, accounts, fakeTester{fn: func(c *gin.Context, id int64, _, _ string) error {
				calls++
				text := goodSVG
				if id == 2 {
					if scenario == "review request fails" {
						emit(c, service.TestEvent{Type: "error", Error: "API returned 429"})
						return nil
					}
					text = "not a rubric"
				}
				emit(c, service.TestEvent{Type: "content", Text: text})
				emit(c, service.TestEvent{Type: "test_complete", Success: true})
				return nil
			}})
			e.renderSVG = func(context.Context, string) ([]byte, int, int, error) {
				if scenario == "render fails" {
					return nil, 0, 0, errors.New("render failed")
				}
				return []byte("png"), 768, 512, nil
			}
			p := newPlan()
			p.Checks = []CheckConfig{{Kind: TestPelicanSVG}}
			p.ScopeType = ScopeAccounts
			p.AccountIDs = []int64{1}
			if scenario == "budget exhausted" {
				p.MaxRequestsPerRun = 1
			}
			st := runOnce(t, e, p)
			require.Zero(t, st.Pass)
			require.Zero(t, st.QualityFail)
			require.Equal(t, 1, st.InfraError)
			require.Equal(t, ReasonReviewError, repo.results[0].Reason)
			require.Zero(t, repo.results[0].Score)
			require.Empty(t, repo.states[skey(1, TestPelicanSVG)].Recent)
			require.Equal(t, calls, st.Requests)
		})
	}
}
