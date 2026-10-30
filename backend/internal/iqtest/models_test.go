package iqtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type catalogFunc func(context.Context, *service.Account) (*service.OpenAIModelsResponse, error)

func (f catalogFunc) FetchIQTestModels(ctx context.Context, a *service.Account) (*service.OpenAIModelsResponse, error) {
	return f(ctx, a)
}

func TestAutoModelUsesEachAccountsLiveEntitlements(t *testing.T) {
	a, b := acct(1), acct(2)
	a.Platform, b.Platform = service.PlatformOpenAI, service.PlatformOpenAI
	repo := newMemRepo()
	e := newTestEngine(repo, []*service.Account{a, b}, fakeTester{fn: func(c *gin.Context, id int64, model, prompt string) error {
		if id == 1 {
			require.Equal(t, "gpt-6-luna", model)
		} else {
			require.Equal(t, "gpt-6-astra", model)
		}
		emit(c, service.TestEvent{Type: "test_start", Model: model})
		emit(c, service.TestEvent{Type: "content", Text: "21"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}})
	e.models = catalogFunc(func(_ context.Context, a *service.Account) (*service.OpenAIModelsResponse, error) {
		body := `{"models":[{"slug":"gpt-6-luna"}]}`
		if a.ID == 2 {
			body = `{"models":[{"slug":"gpt-6.1-sol"},{"slug":"gpt-6-astra"}]}`
		}
		return &service.OpenAIModelsResponse{Body: []byte(body)}, nil
	})
	stats := runOnce(t, e, newPlan())
	require.Equal(t, 2, stats.Pass)
	for _, r := range repo.results {
		require.NotEmpty(t, r.Model)
	}
}

func TestAutoModelRetriesOnlyUnsupportedOnSameAccount(t *testing.T) {
	for _, failure := range []string{"API returned 400: model not supported", "API returned 401: token_revoked", "API returned 429: rate limit", "API returned 504: timeout", "wrong answer"} {
		t.Run(failure, func(t *testing.T) {
			a := acct(1)
			a.Platform = service.PlatformOpenAI
			calls := 0
			repo := newMemRepo()
			e := newTestEngine(repo, []*service.Account{a}, fakeTester{fn: func(c *gin.Context, id int64, model, prompt string) error {
				calls++
				require.Equal(t, int64(1), id)
				emit(c, service.TestEvent{Type: "test_start", Model: model})
				if calls == 1 && failure != "wrong answer" {
					emit(c, service.TestEvent{Type: "error", Error: failure})
					return nil
				}
				answer := "21"
				if failure == "wrong answer" {
					answer = "29"
				}
				emit(c, service.TestEvent{Type: "content", Text: answer})
				emit(c, service.TestEvent{Type: "test_complete", Success: true})
				return nil
			}})
			e.models = catalogFunc(func(context.Context, *service.Account) (*service.OpenAIModelsResponse, error) {
				return &service.OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"gpt-6-astra"},{"slug":"gpt-6-luna"}]}`)}, nil
			})
			stats := runOnce(t, e, newPlan())
			if failure == "API returned 400: model not supported" {
				require.Equal(t, 2, calls)
				require.Equal(t, 1, stats.Pass)
				require.Equal(t, "gpt-6-luna", repo.results[0].Model)
			} else {
				require.Equal(t, 1, calls)
				require.Zero(t, stats.Pass)
			}
		})
	}
}

func TestModelRecommendationsRefreshAndRetainLastGood(t *testing.T) {
	p := newModelPreferences()
	calls := 0
	p.fetch = func(context.Context) ([]byte, error) {
		calls++
		return []byte(`## Recommended models
<ModelDetails slug="gpt-7-astra"/><ModelDetails slug="gpt-7-luna"/>
## Choosing a model
<ModelDetails slug="gpt-5.5"/>`), nil
	}
	require.Equal(t, []string{"gpt-7-astra", "gpt-7-luna"}, p.order(context.Background()))
	p.order(context.Background())
	require.Equal(t, 1, calls)
	p.nextRefresh = time.Time{}
	p.fetch = func(context.Context) ([]byte, error) { return nil, errors.New("offline") }
	require.Equal(t, []string{"gpt-7-astra", "gpt-7-luna"}, p.order(context.Background()))
	ids, err := rankedModels([]byte(`{"models":[{"slug":"gpt-6-astra"},{"slug":"gpt-7-astra"}]}`), p.ids, false)
	require.NoError(t, err)
	require.Equal(t, "gpt-7-astra", ids[0])
}

func TestAutoModelRequiresCatalogAndVisionSupport(t *testing.T) {
	_, err := rankedModels([]byte(`{"models":[{"slug":"gpt-6-astra","input_modalities":["text"]}]}`), nil, true)
	require.Error(t, err)
	ids, err := rankedModels([]byte(`{"models":[{"slug":"gpt-6-astra","visibility":"hide"},{"slug":"gpt-image-2"},{"slug":"gpt-6-luna","input_modalities":["text","image"]}]}`), nil, true)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-luna"}, ids)
}

func TestRecordedModelUsesMappedTestStart(t *testing.T) {
	r := RunCheck(context.Background(), fakeTester{fn: func(c *gin.Context, _ int64, _, _ string) error {
		emit(c, service.TestEvent{Type: "test_start", Model: "actual-upstream-model"})
		emit(c, service.TestEvent{Type: "content", Text: "21"})
		emit(c, service.TestEvent{Type: "test_complete", Success: true})
		return nil
	}}, 1, CheckConfig{Kind: TestCandy, Model: "alias"})
	require.Equal(t, "actual-upstream-model", r.Model)
}
