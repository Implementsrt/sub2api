// Package personalusage contains fork-specific usage extensions, independent of
// upstream billing, scheduling and generated Ent models.
package personalusage

import (
	"math"
	"sort"
	"time"
)

const MaxGap = 5 * time.Minute
const DropThresholdPoints = 20.0

// Observation contains response usage only; no request content or credentials.
type Observation struct {
	ID                  int64
	UserID              int64
	SessionID           string
	AccountID           int64
	Model               string
	CreatedAt           time.Time // Sub2API usage completion/recording time, not request start.
	DurationMs          *int
	InputTokens         int
	CacheReadTokens     int
	CacheCreationTokens int
	Compaction          bool
}

type CacheDrop struct {
	PreviousRequestID  int64     `json:"previous_usage_id"`
	PreviousCreatedAt  time.Time `json:"previous_created_at"`
	GapSeconds         float64   `json:"gap_seconds"`
	PreviousHitRate    float64   `json:"previous_hit_rate"`
	CurrentHitRate     float64   `json:"current_hit_rate"`
	DropPoints         float64   `json:"drop_points"`
	ThresholdPoints    float64   `json:"threshold_points"`
	PreviousInputTotal int64     `json:"previous_input_total"`
	CurrentInputTotal  int64     `json:"current_input_total"`
}

func inputUsage(r Observation) (int64, float64, bool) {
	if r.InputTokens < 0 || r.CacheReadTokens < 0 || r.CacheCreationTokens < 0 {
		return 0, 0, false
	}
	// Sub2API stores uncached input separately for both OpenAI and Anthropic.
	total := int64(r.InputTokens) + int64(r.CacheReadTokens) + int64(r.CacheCreationTokens)
	if total <= 0 {
		return 0, 0, false
	}
	return total, float64(r.CacheReadTokens) / float64(total), true
}

// Detect requires an unambiguous, non-overlapping predecessor in the same
// session. Callers supply neighbors from storage, independent of UI filters.
func Detect(rows []Observation) map[int64]*CacheDrop {
	type identity struct {
		user    int64
		session string
	}
	sessions := make(map[identity][]Observation)
	for _, row := range rows {
		if row.SessionID != "" {
			key := identity{row.UserID, row.SessionID}
			sessions[key] = append(sessions[key], row)
		}
	}
	result := make(map[int64]*CacheDrop)
	for _, calls := range sessions {
		sort.Slice(calls, func(i, j int) bool { return calls[i].CreatedAt.Before(calls[j].CreatedAt) })
		var previous *Observation
		for i := 0; i < len(calls); {
			j := i + 1
			for j < len(calls) && calls[j].CreatedAt.Equal(calls[i].CreatedAt) {
				j++
			}
			if j-i != 1 {
				previous = nil
				i = j
				continue
			}
			current := &calls[i]
			if previous != nil && current.AccountID > 0 && current.AccountID == previous.AccountID && current.Model != "" && current.Model == previous.Model && !current.Compaction && !previous.Compaction {
				gap := current.CreatedAt.Sub(previous.CreatedAt)
				before, oldRate, oldOK := inputUsage(*previous)
				after, newRate, newOK := inputUsage(*current)
				// Sub2API sets created_at after forwarding completes. Estimate
				// the current start by subtracting its duration, and compare it
				// with the previous completion; do not add duration twice.
				ended := previous.DurationMs != nil && *previous.DurationMs >= 0 && current.DurationMs != nil && *current.DurationMs >= 0 && !previous.CreatedAt.After(current.CreatedAt.Add(-time.Duration(*current.DurationMs)*time.Millisecond))
				drop := (oldRate - newRate) * 100
				if oldOK && newOK && ended && gap > 0 && gap <= MaxGap && drop >= DropThresholdPoints-1e-9 && current.CacheReadTokens < previous.CacheReadTokens {
					result[current.ID] = &CacheDrop{previous.ID, previous.CreatedAt, gap.Seconds(), oldRate, newRate, math.Round(drop*100) / 100, DropThresholdPoints, before, after}
				}
			}
			previous = current
			i = j
		}
	}
	return result
}
