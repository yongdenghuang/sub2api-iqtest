package service

import "context"

// FetchIQTestModels reuses live, account-scoped Codex discovery. Refresh an
// expiring OAuth token before discovery, as normal gateway requests do.
func (s *OpenAIGatewayService) FetchIQTestModels(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if current.IsOpenAIOAuth() && !current.IsOpenAIAgentIdentity() {
		token, _, err := s.GetAccessToken(ctx, current)
		if err != nil {
			return nil, err
		}
		latest, err := s.accountRepo.GetByID(ctx, current.ID)
		if err != nil {
			return nil, err
		}
		copy := *latest
		copy.Credentials = make(map[string]any, len(latest.Credentials))
		for k, v := range latest.Credentials {
			copy.Credentials[k] = v
		}
		copy.Credentials["access_token"] = token
		current = &copy
	}
	return s.FetchCodexModelsManifest(ctx, current, "", "")
}
