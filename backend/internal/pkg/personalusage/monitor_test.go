package personalusage

import (
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	duration := 1000
	start := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	base := Observation{ID: 1, UserID: 1, SessionID: "session", AccountID: 7, Model: "gpt", CreatedAt: start, DurationMs: &duration, InputTokens: 10, CacheReadTokens: 90}
	current := base
	current.ID, current.CreatedAt, current.InputTokens, current.CacheReadTokens = 2, start.Add(time.Minute), 30, 70
	tests := []struct {
		name   string
		change func(*Observation, *Observation)
		want   bool
	}{
		{"exact 20 point drop", func(_, _ *Observation) {}, true},
		{"different user", func(_, b *Observation) { b.UserID++ }, false},
		{"different session", func(_, b *Observation) { b.SessionID = "other" }, false},
		{"missing session", func(a, b *Observation) { a.SessionID = ""; b.SessionID = "" }, false},
		{"different account", func(_, b *Observation) { b.AccountID++ }, false},
		{"unknown account", func(a, b *Observation) { a.AccountID = 0; b.AccountID = 0 }, false},
		{"different model", func(_, b *Observation) { b.Model = "claude" }, false},
		{"compaction", func(_, b *Observation) { b.Compaction = true }, false},
		{"after compaction", func(a, _ *Observation) { a.Compaction = true }, false},
		{"exact five minutes", func(_, b *Observation) { b.CreatedAt = start.Add(MaxGap) }, true},
		{"expired", func(_, b *Observation) { b.CreatedAt = start.Add(MaxGap + time.Millisecond) }, false},
		{"unknown duration", func(a, _ *Observation) { a.DurationMs = nil }, false},
		{"unknown current duration", func(_, b *Observation) { b.DurationMs = nil }, false},
		{"overlap", func(_, b *Observation) { v := 60001; b.DurationMs = &v }, false},
		{"ends at start", func(_, b *Observation) { v := 60000; b.DurationMs = &v }, true},
		{"long previous completed request", func(a, _ *Observation) { v := 600000; a.DurationMs = &v }, true},
		{"below threshold", func(_, b *Observation) { b.InputTokens = 29; b.CacheReadTokens = 71 }, false},
		{"same cache reads", func(_, b *Observation) { b.InputTokens = 110; b.CacheReadTokens = 90 }, false},
		{"invalid usage", func(_, b *Observation) { b.InputTokens = -1 }, false},
		{"empty usage", func(_, b *Observation) { b.InputTokens = 0; b.CacheReadTokens = 0 }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, b := base, current
			test.change(&a, &b)
			// Sorting the input must not affect detection (the UI can sort freely).
			alert := Detect([]Observation{b, a})[b.ID]
			if (alert != nil) != test.want {
				t.Fatalf("alert=%v, want=%v", alert, test.want)
			}
			if alert != nil && (alert.PreviousRequestID != a.ID || alert.DropPoints != 20) {
				t.Fatalf("incorrect evidence: %+v", alert)
			}
		})
	}
	for _, tiedAt := range []time.Time{start, current.CreatedAt} {
		tied := base
		tied.ID, tied.CreatedAt = 3, tiedAt
		if alert := Detect([]Observation{base, current, tied})[current.ID]; alert != nil {
			t.Fatal("equal-time batch must not be compared")
		}
	}
	intervening := base
	intervening.ID, intervening.CreatedAt, intervening.Model = 3, start.Add(30*time.Second), "other"
	if Detect([]Observation{base, current, intervening})[current.ID] != nil {
		t.Fatal("must not skip an intervening request")
	}
}
