package service

import "github.com/gin-gonic/gin"

// AccountTestOverrideKey is the gin context key under which callers (the
// iqtest package) pass a custom prompt, Claude token budget, or OpenAI image
// input to the account-test pipeline. HTTP request fields do not populate it.
const AccountTestOverrideKey = "account_test_override"

// AccountTestOverride customises internal account test requests.
type AccountTestOverride struct {
	Prompt    string
	MaxTokens int
	// Internal IQ vision input; never populated from user request headers.
	ImageDataURL string
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

// ApplyAccountTestVisionInput adds the rendered PNG to an OpenAI test request.
// Both text and actual image bytes are sent; an SVG source string is not vision input.
func ApplyAccountTestVisionInput(c *gin.Context, payload map[string]any, chat bool) {
	v, ok := c.Get(AccountTestOverrideKey)
	if !ok {
		return
	}
	ov, ok := v.(AccountTestOverride)
	if !ok || ov.ImageDataURL == "" {
		return
	}
	if chat {
		payload["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "text", "text": ov.Prompt}, {"type": "image_url", "image_url": map[string]any{"url": ov.ImageDataURL, "detail": "high"}},
		}}}
	} else {
		payload["input"] = []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "input_text", "text": ov.Prompt}, {"type": "input_image", "image_url": ov.ImageDataURL, "detail": "high"},
		}}}
	}
}
