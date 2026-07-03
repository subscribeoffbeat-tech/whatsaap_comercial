package automation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"whatsapptool/internal/db"
)

// strPtr is a test helper that returns a pointer to s.
func strPtr(s string) *string { return &s }

// ── matchesRule ───────────────────────────────────────────────────────────────

func TestMatchesRule_Keyword_Exact(t *testing.T) {
	rule := &db.AutomationRule{
		TriggerType:  "keyword",
		Keyword:      strPtr("PRICE"),
		KeywordMatch: "exact",
	}
	assert.True(t, matchesRule(rule, "PRICE", false, time.Now(), false))
	assert.True(t, matchesRule(rule, "price", false, time.Now(), false), "case-insensitive")
	assert.False(t, matchesRule(rule, "what is the PRICE?", false, time.Now(), false), "substring not exact")
	assert.False(t, matchesRule(rule, "", false, time.Now(), false))
}

func TestMatchesRule_Keyword_Contains(t *testing.T) {
	rule := &db.AutomationRule{
		TriggerType:  "keyword",
		Keyword:      strPtr("book"),
		KeywordMatch: "contains",
	}
	assert.True(t, matchesRule(rule, "I want to book a slot", false, time.Now(), false))
	assert.True(t, matchesRule(rule, "BOOK NOW", false, time.Now(), false), "case-insensitive")
	assert.False(t, matchesRule(rule, "call me", false, time.Now(), false))
}

func TestMatchesRule_Keyword_EmptyKeyword(t *testing.T) {
	rule := &db.AutomationRule{
		TriggerType:  "keyword",
		Keyword:      strPtr(""),
		KeywordMatch: "exact",
	}
	assert.False(t, matchesRule(rule, "anything", false, time.Now(), false))
}

func TestMatchesRule_Keyword_NilKeyword(t *testing.T) {
	rule := &db.AutomationRule{
		TriggerType:  "keyword",
		Keyword:      nil,
		KeywordMatch: "exact",
	}
	assert.False(t, matchesRule(rule, "anything", false, time.Now(), false))
}

func TestMatchesRule_Welcome_NewContact(t *testing.T) {
	rule := &db.AutomationRule{TriggerType: "welcome"}
	assert.True(t, matchesRule(rule, "hi", true, time.Now(), false))
	assert.False(t, matchesRule(rule, "hi", false, time.Now(), false), "not new contact")
}

func TestMatchesRule_Away_QuietHours(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	rule := &db.AutomationRule{TriggerType: "away"}

	nightIST := time.Date(2024, 1, 1, 22, 0, 0, 0, ist)  // 22:00 IST — quiet
	dayIST   := time.Date(2024, 1, 1, 12, 0, 0, 0, ist)  // 12:00 IST — not quiet

	assert.True(t, matchesRule(rule, "hi", false, nightIST, true))
	assert.False(t, matchesRule(rule, "hi", false, dayIST, false))
}

func TestMatchesRule_Stop_NeverFires(t *testing.T) {
	rule := &db.AutomationRule{TriggerType: "stop"}
	// STOP is handled in inbox handler, never by the engine.
	assert.False(t, matchesRule(rule, "STOP", false, time.Now(), false))
}

// ── Priority — first match wins ───────────────────────────────────────────────

// TestFirstMatchWins simulates the rule ordering: given two keyword rules
// both matching "hi", the one with lower priority number fires first.
func TestFirstMatchWins(t *testing.T) {
	textP0 := "Reply from priority-0 rule"
	textP1 := "Reply from priority-1 rule"

	rules := []*db.AutomationRule{
		{ID: 1, TriggerType: "keyword", Keyword: strPtr("hi"), KeywordMatch: "contains", Priority: 0, Active: true, ResponseText: &textP0},
		{ID: 2, TriggerType: "keyword", Keyword: strPtr("hi"), KeywordMatch: "contains", Priority: 1, Active: true, ResponseText: &textP1},
	}

	// Both match "hi"; the first (priority 0) should win.
	now := time.Now()
	first := -1
	for _, r := range rules {
		if matchesRule(r, "hi there", false, now, false) {
			first = int(r.ID)
			break
		}
	}
	assert.Equal(t, 1, first, "priority-0 rule should fire first")
}

// ── STOP always first in ListActiveRules ordering ─────────────────────────────

// TestStopAlwaysFirst verifies that a STOP-type rule sorts before other types.
func TestStopAlwaysFirst(t *testing.T) {
	rules := []*db.AutomationRule{
		{ID: 2, TriggerType: "keyword",  Priority: 0},
		{ID: 1, TriggerType: "welcome",  Priority: 0},
		{ID: 3, TriggerType: "stop",     Priority: 0},
	}

	// Simulate the ORDER BY from ListActiveRules:
	// CASE trigger_type WHEN 'stop' THEN 0 ELSE 1 END, priority ASC, id ASC
	sorted := sortedByEngineOrder(rules)
	assert.Equal(t, "stop", sorted[0].TriggerType, "stop must be first")
}

// sortedByEngineOrder mirrors the SQL ORDER BY used in ListActiveRules.
func sortedByEngineOrder(rules []*db.AutomationRule) []*db.AutomationRule {
	out := make([]*db.AutomationRule, len(rules))
	copy(out, rules)
	// Stable insertion sort matching: CASE stop THEN 0 ELSE 1 END, priority, id
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && engineKey(out[j]) < engineKey(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func engineKey(r *db.AutomationRule) int64 {
	tier := int64(1)
	if r.TriggerType == "stop" {
		tier = 0
	}
	return tier*1_000_000_000 + int64(r.Priority)*1_000_000 + r.ID
}
