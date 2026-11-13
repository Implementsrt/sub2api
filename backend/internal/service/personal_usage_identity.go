package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/personalusage"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ExtractPersonalUsageIdentity runs on the original client frame before async
// recording. Upstream fingerprint convergence must never supply this identity.
func ExtractPersonalUsageIdentity(c *gin.Context, body []byte) *personalusage.Identity {
	metadata := openAIWSExecutionTurnMetadata(c, body)
	threadID := metadata.ThreadID
	subagent := openAIWSExecutionSubagent(c, body)
	if c != nil && c.Request != nil {
		if id := c.GetHeader(openAIWSThreadIDHeader); id != "" {
			threadID = id
		}
	}
	if threadID == "" {
		threadID = gjson.GetBytes(body, "client_metadata.thread_id").String()
	}
	if threadID == "" {
		threadID = codexTurnMetadataThreadID(gjson.GetBytes(body, "client_metadata."+openAIWSTurnMetadataHeader).String())
	}
	if subagent == "" {
		raw := gjson.GetBytes(body, "client_metadata."+openAIWSTurnMetadataHeader).String()
		if c != nil && c.Request != nil && c.GetHeader(openAIWSTurnMetadataHeader) != "" {
			raw = c.GetHeader(openAIWSTurnMetadataHeader)
		}
		subagent = strings.ToLower(gjson.Get(raw, "subagent_kind").String())
	}
	identity := &personalusage.Identity{
		ThreadID: sanitizeSessionID(threadID),
		Kind:     sanitizeSessionID(metadata.RequestKind),
		Subagent: sanitizeSessionID(subagent),
		Source:   "client_metadata_v1",
	}
	// Reject malformed discriminators instead of folding a secondary request
	// into the main lane after sanitization removed its kind/subagent.
	if (metadata.RequestKind != "" && identity.Kind == "") || (strings.TrimSpace(subagent) != "" && identity.Subagent == "") {
		identity.ThreadID = ""
	}
	return identity
}

func submitPersonalUsageIdentity(repo UsageLogRepository, row *UsageLog) {
	if row == nil || row.ForkIdentity == nil {
		return
	}
	if writer, ok := repo.(interface {
		RecordPersonalUsageIdentity(context.Context, *UsageLog) error
	}); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := writer.RecordPersonalUsageIdentity(ctx, row); err != nil {
			logger.LegacyPrintf("personal_usage", "execution metadata unavailable")
		}
	}
}

type personalUsageIdentityReader interface {
	ListPersonalUsageIdentities(context.Context, int64, []UsageLog) (map[int64]*personalusage.Identity, error)
}

func (s *UsageService) PersonalUsageIdentities(ctx context.Context, userID int64, rows []UsageLog) (map[int64]*personalusage.Identity, error) {
	if reader, ok := s.usageRepo.(personalUsageIdentityReader); ok {
		return reader.ListPersonalUsageIdentities(ctx, userID, rows)
	}
	return map[int64]*personalusage.Identity{}, nil
}

func PersonalUsageTitleID(row UsageLog, identity *personalusage.Identity) string {
	if identity != nil && identity.ThreadID != "" {
		return identity.ThreadID
	}
	if identity != nil && identity.Subagent != "" {
		// A parent's correlation header is not the unnamed review/agent's
		// title. Display its known role with an unresolved-thread marker.
		return ""
	}
	if row.SessionID != nil {
		return *row.SessionID
	}
	return ""
}

func PersonalUsageMonitoringStatus(row UsageLog, identity *personalusage.Identity, status string) string {
	if status != "ready" {
		return status
	}
	if identity == nil || identity.Scope() == "" {
		return "identity_missing"
	}
	if strings.EqualFold(identity.Subagent, "guardian") {
		return "not_comparable"
	}
	return status
}
