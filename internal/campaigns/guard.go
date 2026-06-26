// Package campaigns implements audience building, rate limiting, cost
// calculation, and the River queue worker for broadcast campaigns.
package campaigns

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
)

// istLoc is Asia/Kolkata (UTC+5:30). Loaded once at startup.
var istLoc *time.Location

func init() {
	var err error
	istLoc, err = time.LoadLocation("Asia/Kolkata")
	if err != nil {
		istLoc = time.FixedZone("IST", 5*60*60+30*60)
	}
}

// IsQuietHours reports whether t falls in the default quiet window (21:00–09:00
// IST). Business-initiated sends (campaigns) are blocked during quiet hours.
// Prefer IsQuietHoursCfg where a DB pool is available so the admin-configured
// window is honoured.
func IsQuietHours(t time.Time) bool {
	return InQuietWindow(t, 21*60, 9*60)
}

// IsQuietHoursCfg reports whether t is in the admin-configured quiet window
// (config keys quiet_hours_start_ist / quiet_hours_end_ist, "HH:MM"), falling
// back to 21:00–09:00 IST if unset or unparseable.
func IsQuietHoursCfg(ctx context.Context, pool *pgxpool.Pool, t time.Time) bool {
	start, end := quietWindow(ctx, pool)
	return InQuietWindow(t, start, end)
}

// quietWindow returns the configured quiet-hours start/end as minutes-of-day.
func quietWindow(ctx context.Context, pool *pgxpool.Pool) (startMin, endMin int) {
	startMin, endMin = 21*60, 9*60 // defaults
	if s, err := db.GetConfigString(ctx, pool, "quiet_hours_start_ist"); err == nil {
		if m, ok := parseHHMM(s); ok {
			startMin = m
		}
	}
	if s, err := db.GetConfigString(ctx, pool, "quiet_hours_end_ist"); err == nil {
		if m, ok := parseHHMM(s); ok {
			endMin = m
		}
	}
	return startMin, endMin
}

// InQuietWindow reports whether t (in IST) falls within [startMin, endMin),
// handling windows that wrap past midnight (e.g. 21:00 → 09:00).
func InQuietWindow(t time.Time, startMin, endMin int) bool {
	ist := t.In(istLoc)
	h, m, _ := ist.Clock()
	mins := h*60 + m
	if startMin == endMin {
		return false // no quiet window
	}
	if startMin < endMin {
		return mins >= startMin && mins < endMin
	}
	return mins >= startMin || mins < endMin // wraps midnight
}

func parseHHMM(s string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// IsUSNumber reports whether phone is a +1 (US/Canada) number.
// US numbers are excluded from marketing campaigns since Apr 2025.
func IsUSNumber(phone string) bool {
	return strings.HasPrefix(phone, "+1")
}

// LimitGuardCheck returns true if sending is allowed given daily usage.
// sent is the number of outbound messages already sent today.
// cap is the configured daily cap (90% of tier).
func LimitGuardCheck(sent, cap int64) bool {
	return sent < cap
}

// ── Audience building ─────────────────────────────────────────────────────────

// SkipReport describes how many contacts were excluded and why.
type SkipReport struct {
	NotOptedIn int // pre-filtered at DB layer (opted_in = true required)
	USNumber   int // +1 numbers excluded from marketing
	FreqCap    int // contacted within freqCapHours
	Total      int // sum of all skipped
	// Skipped maps a skipped contact ID to a human-readable reason, so the
	// skipped recipients can be recorded and shown in the campaign report.
	Skipped map[string]string
}

// Recipient is an eligible campaign recipient after all filters pass.
type Recipient struct {
	ContactID    string
	WAPhone      string
	Name         string
	CustomFields map[string]any
}

// FilterAudience applies all hard exclusion rules to a list of contacts.
// It is a pure function, usable in tests without a database.
// freqCapped is a set of contact IDs that received a marketing message
// within the configured frequency cap window.
func FilterAudience(contacts []db.Contact, freqCapped map[string]bool, category string) (eligible []Recipient, report SkipReport) {
	report.Skipped = map[string]string{}
	for _, c := range contacts {
		if category == "marketing" {
			if IsUSNumber(c.WAPhone) {
				report.USNumber++
				report.Total++
				report.Skipped[c.ID] = "US (+1) number — excluded from marketing since Apr 2025"
				continue
			}
			if freqCapped[c.ID] {
				report.FreqCap++
				report.Total++
				report.Skipped[c.ID] = "Frequency cap — already received a marketing message in the last 24h"
				continue
			}
		}
		eligible = append(eligible, Recipient{
			ContactID:    c.ID,
			WAPhone:      c.WAPhone,
			Name:         c.Name,
			CustomFields: c.CustomFields,
		})
	}
	return
}

// BuildAudienceFromIDs builds the eligible audience from an explicit set of
// contact IDs (the owner's refined selection on the audience step). It applies
// the same compliance filters (opted-in at the DB layer, US/frequency in
// FilterAudience) so a hand-picked list can't bypass the hard rules.
func BuildAudienceFromIDs(
	ctx context.Context, pool *pgxpool.Pool,
	contactIDs []string, category string, freqCapHours int,
) (eligible []Recipient, report SkipReport, err error) {
	contacts, err := db.GetAudienceContactsByIDs(ctx, pool, contactIDs)
	if err != nil {
		return nil, report, err
	}
	var freqCapped map[string]bool
	if category == "marketing" && len(contacts) > 0 {
		ids := make([]string, len(contacts))
		for i, c := range contacts {
			ids[i] = c.ID
		}
		freqCapped, err = db.FreqCappedContactIDs(ctx, pool, ids, freqCapHours)
		if err != nil {
			return nil, report, err
		}
	}
	eligible, report = FilterAudience(contacts, freqCapped, category)
	return eligible, report, nil
}

// BuildAudienceAny is like BuildAudience but matches contacts with ANY of the
// given tags (union) rather than ALL. If tagIDs is empty, all opted-in contacts
// are included (no tag filter).
func BuildAudienceAny(
	ctx context.Context, pool *pgxpool.Pool,
	tagIDs []int64, category string, freqCapHours int,
) (eligible []Recipient, report SkipReport, err error) {
	contacts, err := db.GetAudienceContactsAny(ctx, pool, tagIDs)
	if err != nil {
		return nil, report, err
	}
	var freqCapped map[string]bool
	if category == "marketing" && len(contacts) > 0 {
		ids := make([]string, len(contacts))
		for i, c := range contacts {
			ids[i] = c.ID
		}
		freqCapped, err = db.FreqCappedContactIDs(ctx, pool, ids, freqCapHours)
		if err != nil {
			return nil, report, err
		}
	}
	eligible, report = FilterAudience(contacts, freqCapped, category)
	return eligible, report, nil
}

// BuildAudience fetches opted-in contacts matching the segment, applies all
// exclusion rules, and returns the eligible list + skip counts.
func BuildAudience(
	ctx context.Context, pool *pgxpool.Pool,
	segmentTags, excludeTags []int64,
	category string,
	freqCapHours int,
) (eligible []Recipient, report SkipReport, err error) {
	contacts, err := db.GetAudienceContacts(ctx, pool, segmentTags, excludeTags)
	if err != nil {
		return nil, report, err
	}

	var freqCapped map[string]bool
	if category == "marketing" && len(contacts) > 0 {
		ids := make([]string, len(contacts))
		for i, c := range contacts {
			ids[i] = c.ID
		}
		freqCapped, err = db.FreqCappedContactIDs(ctx, pool, ids, freqCapHours)
		if err != nil {
			return nil, report, err
		}
	}

	eligible, report = FilterAudience(contacts, freqCapped, category)
	return eligible, report, nil
}
