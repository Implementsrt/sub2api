package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestCodexHTTPPromptCacheForward(t *testing.T) {
	instructions := strings.Repeat("稳定的 Codex instructions with \"quotes\" and <tags>.\r\n", 150)
	const priorInput = `[{"role":"developer","content":"project rules"},{"type":"reasoning","encrypted_content":"test","summary":[]},{"role":"user","content":"hello"}]`
	for _, mode := range []string{"normal", "passthrough"} {
		t.Run(mode, func(t *testing.T) {
			body, err := sjson.Set(`{"model":"gpt-6.2-sol","store":false,"stream":true,"prompt_cache_key":"session","input":`+priorInput+`}`, "instructions", instructions)
			require.NoError(t, err)
			account := &Account{
				ID: 501, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key"},
				Extra:       map[string]any{"openai_passthrough": mode == "passthrough"},
			}
			upstream := instructionsEchoUpstream(`"` + codexCacheableInstructionsReminder + `"`)
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Request.Header.Set("User-Agent", "codex_cli_rs/0.180.0")
			SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)

			_, err = svc.Forward(context.Background(), ctx, account, []byte(body))
			require.NoError(t, err)
			require.Equal(t, mode == "passthrough", ctx.GetBool("openai_passthrough"))
			wire := gjson.ParseBytes(upstream.lastBody)
			require.Equal(t, codexCacheableInstructionsReminder, wire.Get("instructions").Str)
			require.Equal(t, "developer", wire.Get("input.0.role").Str)
			require.Equal(t, instructions, wire.Get("input.0.content.0.text").Str)
			require.Equal(t, "session", wire.Get("prompt_cache_key").Str)
			items := wire.Get("input").Array()
			originalItems := gjson.Parse(priorInput).Array()
			require.Len(t, items, len(originalItems)+1)
			for index, item := range originalItems {
				require.JSONEq(t, item.Raw, items[index+1].Raw)
			}
			require.Equal(t, instructions, sseResponseInstructions(t, recorder.Body.String(), "response.completed").Str)
		})
	}
}

func TestCodexHTTPPromptCacheGuards(t *testing.T) {
	longPrompt := strings.Repeat("stable prompt ", 400)
	body, err := sjson.Set(`{"model":"gpt-6.2-astra","store":false,"input":"hello"}`, "instructions", longPrompt)
	require.NoError(t, err)
	svc := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformOpenAI}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("originator", "codex_cli_rs")
	for _, test := range []struct {
		name  string
		field string
		value any
	}{
		{"older model", "model", "gpt-6.1-sol"},
		{"stored response", "store", true},
		{"default store", "store", nil},
		{"previous response", "previous_response_id", "resp_earlier"},
		{"conversation", "conversation", "conv_earlier"},
		{"turn-scoped instruction", "instructions", "Only for this turn."},
	} {
		t.Run(test.name, func(t *testing.T) {
			unchanged, err := sjson.Set(body, test.field, test.value)
			require.NoError(t, err)
			outbound, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(unchanged), []byte(unchanged))
			require.NoError(t, err)
			require.Equal(t, unchanged, string(outbound))
		})
	}
	clientBody, err := sjson.Set(body, "previous_response_id", "resp_removed_by_normalization")
	require.NoError(t, err)
	outbound, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(clientBody), []byte(body))
	require.NoError(t, err)
	require.Equal(t, body, string(outbound))

	actual, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(body), []byte(body))
	require.NoError(t, err)
	require.Equal(t, longPrompt, gjson.GetBytes(actual, "input.0.content.0.text").Str)
	require.Equal(t, "user", gjson.GetBytes(actual, "input.1.role").Str)
	require.Equal(t, "hello", gjson.GetBytes(actual, "input.1.content.0.text").Str)
	secondPass, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(body), actual)
	require.NoError(t, err)
	require.Equal(t, actual, secondPass)
	ctx.Request.Header.Del("originator")
	otherClient, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(body), []byte(body))
	require.NoError(t, err)
	require.Equal(t, body, string(otherClient))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	ctx.Request.Header.Set("originator", "codex_cli_rs")
	compact, err := svc.prepareCodexHTTPPromptCache(ctx, account, []byte(body), []byte(body))
	require.NoError(t, err)
	require.Equal(t, body, string(compact))
}
