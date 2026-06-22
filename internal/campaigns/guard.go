// Package campaigns implements audience building, rate limiting, cost
// calculation, and the River queue worker for broadcast campaigns.
package campaigns

import (
	"context"
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

// IsQuietHours reports whether t falls in the quiet window (21:00–09:00 IST).
// Business-initiated sends (campaigns) are blocked during quiet hours.
func IsQuietHours(t time.Time) bool {
	ist := t.In(istLoc)
	h, m, _ := ist.Clock()
	mins := h*60 + m
	return mins >= 21*60 || mins < 9*60
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
	for _, c := range contacts {
		if category == "marketing" {
			if IsUSNumber(c.WAPhone) {
				report.USNumber++
				report.Total++
				continue
			}
			if freqCapped[c.ID] {
				report.FreqCap++
				report.Total++
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
