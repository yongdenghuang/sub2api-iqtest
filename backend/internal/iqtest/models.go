package iqtest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const AutoModel = "auto"
const modelRecommendationsURL = "https://learn.chatgpt.com/docs/models.md"

// ModelCatalog uses the account's credentials and proxy, not a shared account.
// The host service caches account-specific manifests for one minute.
type ModelCatalog interface {
	FetchIQTestModels(context.Context, *service.Account) (*service.OpenAIModelsResponse, error)
}

type modelPreferences struct {
	mu          sync.Mutex
	nextRefresh time.Time
	ids         []string
	fetch       func(context.Context) ([]byte, error)
}

func newModelPreferences() *modelPreferences {
	return &modelPreferences{ids: []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6.1-terra", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}, fetch: fetchModelRecommendations}
}

func fetchModelRecommendations(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelRecommendationsURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("model recommendations unavailable")
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

var recommendedModelRE = regexp.MustCompile(`slug="(gpt-[a-z0-9.-]+)"`)

func parseModelRecommendations(body []byte) []string {
	text := string(body)
	_, text, ok := strings.Cut(text, "## Recommended models")
	if !ok {
		return nil
	}
	text, _, _ = strings.Cut(text, "## Choosing a model")
	var ids []string
	seen := map[string]bool{}
	for _, m := range recommendedModelRE.FindAllStringSubmatch(text, 32) {
		if !seen[m[1]] {
			ids = append(ids, m[1])
			seen[m[1]] = true
		}
	}
	return ids
}

// Refresh official capability recommendations on demand every six hours.
// A changed/unavailable document retains the last good policy, never an empty one.
func (p *modelPreferences) order(ctx context.Context) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().After(p.nextRefresh) {
		p.nextRefresh = time.Now().Add(10 * time.Minute)
		if body, err := p.fetch(ctx); err == nil {
			if ids := parseModelRecommendations(body); len(ids) >= 2 {
				p.ids = ids
				p.nextRefresh = time.Now().Add(6 * time.Hour)
			}
		}
	}
	return append([]string(nil), p.ids...)
}

type catalogModel struct {
	Slug            string   `json:"slug"`
	Visibility      string   `json:"visibility"`
	Priority        int      `json:"priority"`
	InputModalities []string `json:"input_modalities"`
	Available       *bool    `json:"available"`
}

func rankedModels(body []byte, order []string, vision bool) ([]string, error) {
	var catalog struct {
		Models []catalogModel `json:"models"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, err
	}
	ranks := map[string]int{}
	for i, id := range order {
		ranks[id] = i
	}
	rank := func(id string) int {
		if r, ok := ranks[id]; ok {
			return r
		}
		return len(order)
	}
	sort.SliceStable(catalog.Models, func(i, j int) bool {
		a, b := catalog.Models[i], catalog.Models[j]
		if rank(a.Slug) != rank(b.Slug) {
			return rank(a.Slug) < rank(b.Slug)
		}
		return a.Priority < b.Priority
	})
	var ids []string
	seen := map[string]bool{}
	for _, m := range catalog.Models {
		id := strings.TrimSpace(m.Slug)
		if id == "" || seen[id] || m.Visibility == "hide" || (m.Available != nil && !*m.Available) {
			continue
		}
		// These tests need text generation, not dedicated media/realtime models.
		if containsAny(strings.ToLower(id), "image", "audio", "realtime", "embedding", "moderation") {
			continue
		}
		if vision && !containsFold(m.InputModalities, "image") {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("no available model supports this test")
	}
	return ids, nil
}

func isAutoModel(model string) bool {
	return strings.TrimSpace(model) == "" || strings.EqualFold(strings.TrimSpace(model), AutoModel)
}

func (e *Engine) testModels(ctx context.Context, a *service.Account, model string, vision bool) ([]string, error) {
	if !isAutoModel(model) {
		return []string{strings.TrimSpace(model)}, nil
	}
	if !a.IsOpenAI() {
		return []string{DefaultModel}, nil
	}
	if e.models == nil {
		return nil, errors.New("account model discovery is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	manifest, err := e.models.FetchIQTestModels(ctx, a)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, errors.New("empty account model catalog")
	}
	return rankedModels(manifest.Body, e.modelPreferences.order(ctx), vision)
}
