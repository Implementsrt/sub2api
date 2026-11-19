package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexCacheableInstructionsMinBytes = 4096
const codexCacheableInstructionsReminder = "Follow the developer instructions in the input."

func (s *OpenAIGatewayService) prepareCodexHTTPPromptCache(c *gin.Context, account *Account, clientBody, upstreamBody []byte) ([]byte, error) {
	if !account.IsOpenAI() || c == nil || openAIResponsesRequestPathSuffix(c) != "" {
		return upstreamBody, nil
	}
	if openai.GPT62ModelID(lastOpenAIModelSegment(gjson.GetBytes(upstreamBody, "model").String())) == "" {
		return upstreamBody, nil
	}
	if !openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) &&
		(s.cfg == nil || !s.cfg.Gateway.ForceCodexCLI) {
		return upstreamBody, nil
	}
	if gjson.GetBytes(upstreamBody, "store").Type != gjson.False || gjson.GetBytes(clientBody, "store").Type == gjson.True ||
		codexPromptCacheUsesServerHistory(clientBody) || codexPromptCacheUsesServerHistory(upstreamBody) {
		return upstreamBody, nil
	}
	return moveCodexInstructionsToInputPrefix(upstreamBody)
}

func codexPromptCacheUsesServerHistory(body []byte) bool {
	return gjson.GetBytes(body, "previous_response_id").Type != gjson.Null || gjson.GetBytes(body, "conversation").Type != gjson.Null
}

func moveCodexInstructionsToInputPrefix(body []byte) ([]byte, error) {
	instructions := gjson.GetBytes(body, "instructions")
	if instructions.Type != gjson.String || len(strings.TrimSpace(instructions.Str)) < codexCacheableInstructionsMinBytes {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() && input.Type != gjson.String {
		return body, nil
	}
	developer, err := codexPromptCacheMessage("developer", instructions.Str)
	if err != nil {
		return nil, err
	}
	var history string
	if input.IsArray() {
		history = strings.TrimSpace(input.Raw[1 : len(input.Raw)-1])
	} else {
		user, err := codexPromptCacheMessage("user", input.Str)
		if err != nil {
			return nil, err
		}
		history = string(user)
	}
	prefix := "[" + string(developer)
	if history != "" {
		prefix += "," + history
	}
	updated, err := sjson.SetRawBytes(body, "input", []byte(prefix+"]"))
	if err != nil {
		return nil, err
	}
	return sjson.SetBytes(updated, "instructions", codexCacheableInstructionsReminder)
}

func codexPromptCacheMessage(role, text string) ([]byte, error) {
	return marshalOpenAIUpstreamJSON(map[string]any{
		"type":    "message",
		"role":    role,
		"content": []map[string]string{{"type": "input_text", "text": text}},
	})
}
