package service

import "github.com/gin-gonic/gin"

// AccountTestOverrideKey is the gin context key under which callers (the
// iqtest package) pass a custom prompt and token budget to the account-test
// pipeline. Only the Claude-protocol test path reads it; other paths already
// take the prompt as an argument.
const AccountTestOverrideKey = "account_test_override"

// AccountTestOverride customises the Claude-protocol account test request.
type AccountTestOverride struct {
	Prompt    string
	MaxTokens int
}

// ApplyAccountTestOverride rewrites the user prompt and max_tokens of a test
// payload built by createTestPayload when an override is present on c.
// Without an override it leaves the payload untouched.
func ApplyAccountTestOverride(c *gin.Context, payload map[string]any) {
	v, ok := c.Get(AccountTestOverrideKey)
	if !ok {
		return
	}
	ov, ok := v.(AccountTestOverride)
	if !ok {
		return
	}
	if ov.MaxTokens > 0 {
		payload["max_tokens"] = ov.MaxTokens
	}
	if ov.Prompt == "" {
		return
	}
	msgs, ok := payload["messages"].([]map[string]any)
	if !ok || len(msgs) == 0 {
		return
	}
	content, ok := msgs[0]["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		return
	}
	content[0]["text"] = ov.Prompt
}
