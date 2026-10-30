//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIReauthVersionPreventsOldSnapshotFromRepopulatingTokenCache(t *testing.T) {
	old := &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "revoked-token", "_token_version": int64(10), "expires_at": time.Now().Add(time.Hour).Unix(),
	}}
	latest := *old
	latest.Credentials = map[string]any{
		"access_token": "reauthorized-token", "_token_version": int64(11), "expires_at": time.Now().Add(time.Hour).Unix(),
	}
	cache := newOpenAITokenCacheStub()
	provider := NewOpenAITokenProvider(&updateAccountCredsRepoStub{account: &latest}, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), old)
	require.NoError(t, err)
	require.Equal(t, "reauthorized-token", token)
	require.Empty(t, cache.tokens[OpenAITokenCacheKey(old)], "stale requests must not restore the revoked cache entry")
	token, err = provider.GetAccessToken(context.Background(), &latest)
	require.NoError(t, err)
	require.Equal(t, "reauthorized-token", token)
	require.Equal(t, token, cache.tokens[OpenAITokenCacheKey(&latest)])
}
