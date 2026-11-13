package personalusage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Identity records client execution metadata only, never prompts or credentials.
// SessionID remains the original correlation header on usage_logs.
type Identity struct {
	ThreadID string `json:"thread_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Subagent string `json:"subagent,omitempty"`
	Source   string `json:"source,omitempty"`
}

type IdentityDisplay struct {
	ThreadID       string `json:"thread_id,omitempty"`
	ParentThreadID string `json:"parent_thread_id,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Subagent       string `json:"subagent,omitempty"`
	Match          string `json:"match"`
}

func DisplayIdentity(identity *Identity, info SessionInfo) IdentityDisplay {
	display := IdentityDisplay{Match: "session_only"}
	if identity == nil {
		return display
	}
	display.ThreadID, display.Kind, display.Subagent = identity.ThreadID, identity.Kind, identity.Subagent
	if identity.ThreadID != "" {
		display.Match = "thread"
		display.ParentThreadID = info.ParentThreadID
		if info.Kind == "subagent" || info.Kind == "guardian" {
			display.Subagent = info.Kind
		}
	} else if identity.Subagent != "" {
		display.Match = "unresolved"
	}
	return display
}

// Scope refuses session-only identities: several agents can share that header.
// A guardian without its own thread cannot safely be compared even to another
// guardian request in the same window.
func (i *Identity) Scope() string {
	if i == nil || i.ThreadID == "" {
		return ""
	}
	kind := i.Kind
	switch kind {
	case "", "turn", "prewarm", "compaction":
		kind = "turn"
	}
	b, _ := json.Marshal([]string{i.ThreadID, kind, i.Subagent})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
