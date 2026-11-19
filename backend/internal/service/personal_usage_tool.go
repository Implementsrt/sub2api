package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// SearchClient carries execution metadata but usually omits session headers.
// Its top-level id is a correlation fallback, never proof of an execution thread.
// Keep this separate from routing and upstream cache/session identity.
func ExtractPersonalUsageToolIdentity(c *gin.Context, body []byte) (string, *personalusage.Identity) {
	identity := ExtractPersonalUsageIdentity(c, body)
	identity.Kind = "tool"
	sessionID := ExtractClientSessionID(c)
	if sessionID == "" {
		sessionID = identity.ThreadID
	}
	if sessionID == "" {
		if id := gjson.GetBytes(body, "id"); id.Type == gjson.String {
			sessionID = sanitizeSessionID(id.String())
		}
	}
	return sessionID, identity
}

// Only known tool endpoints/metadata qualify; sync also includes ordinary model
// requests, so transport type alone must not classify a request as a tool.
func IsPersonalUsageTool(row UsageLog, identity *personalusage.Identity) bool {
	if identity != nil && identity.Kind == "tool" {
		return true
	}
	if row.InboundEndpoint == nil {
		return false
	}
	switch strings.TrimRight(*row.InboundEndpoint, "/") {
	case "/alpha/search", "/v1/alpha/search", "/backend-api/codex/alpha/search":
		return true
	}
	return false
}

// Apply the same display rules to new sidecar identities and historical rows
// that already retained an explicit session ID. Do not invent missing IDs.
func PersonalUsageDisplayIdentity(row UsageLog, identity *personalusage.Identity, info personalusage.SessionInfo) personalusage.IdentityDisplay {
	display := personalusage.DisplayIdentity(identity, info)
	if IsPersonalUsageTool(row, identity) {
		display.Kind = "tool"
		if display.ThreadID == "" {
			display.Match = "related_session"
		}
	}
	return display
}
