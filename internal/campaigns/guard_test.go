package campaigns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"whatsapptool/internal/db"
)

// ── IsQuietHours ──────────────────────────────────────────────────────────────

func TestIsQuietHours(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	tests := []struct {
		name  string
		h, m  int
		quiet bool
	}{
		{"midnight", 0, 0, true},
		{"just after midnight", 0, 30, true},
		{"early morning", 5, 0, true},
		{"just before opening", 8, 59, true},
		{"opening time", 9, 0, false},
		{"midday", 12, 0, false},
		{"afternoon", 15, 30, false},
		{"evening", 18, 0, false},
		{"just before close", 20, 59, false},
		{"closing time", 21, 0, true},
		{"late night", 23, 30, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Construct a time in IST with the given hour/minute.
			base := time.Date(2024, 1, 1, tc.h, tc.m, 0, 0, ist)
			assert.Equal(t, tc.quiet, IsQuietHours(base),
				"IST %02d:%02d should quiet=%v", tc.h, tc.m, tc.quiet)
		})
	}
}

// ── IsUSNumber ────────────────────────────────────────────────────────────────

func TestIsUSNumber(t *testing.T) {
	assert.True(t, IsUSNumber("+12025551234"), "+1 US")
	assert.True(t, IsUSNumber("+14155550100"), "+1 CA")
	assert.False(t, IsUSNumber("+919876543210"), "India +91")
	assert.False(t, IsUSNumber("+447911123456"), "UK +44")
	assert.True(t, IsUSNumber("+11234567890"), "+1 prefix → US/CA")
	assert.False(t, IsUSNumber("+9111111111"), "India +91 is not US")
}

// ── LimitGuardCheck ───────────────────────────────────────────────────────────

func TestLimitGuardCheck(t *testing.T) {
	assert.True(t, LimitGuardCheck(0, 1000), "zero sent: allowed")
	assert.True(t, LimitGuardCheck(899, 900), "just under cap: allowed")
	assert.False(t, LimitGuardCheck(900, 900), "at cap: blocked")
	assert.False(t, LimitGuardCheck(950, 900), "over cap: blocked")
}

// ── FilterAudience ────────────────────────────────────────────────────────────

func TestFilterAudience_USExclusion(t *testing.T) {
	contacts := []db.Contact{
		{ID: "a", WAPhone: "+919876543210", OptedIn: true},
		{ID: "b", WAPhone: "+12025551234", OptedIn: true},  // US — excluded
		{ID: "c", WAPhone: "+447911123456", OptedIn: true},
	}

	eligible, report := FilterAudience(contacts, nil, "marketing")

	assert.Len(t, eligible, 2)
	assert.Equal(t, "a", eligible[0].ContactID)
	assert.Equal(t, "c", eligible[1].ContactID)
	assert.Equal(t, 1, report.USNumber)
	assert.Equal(t, 1, report.Total)
}

func TestFilterAudience_FreqCap(t *testing.T) {
	contacts := []db.Contact{
		{ID: "a", WAPhone: "+919876543210", OptedIn: true},
		{ID: "b", WAPhone: "+919876543211", OptedIn: true},
		{ID: "c", WAPhone: "+919876543212", OptedIn: true},
	}
	freqCapped := map[string]bool{"b": true}

	eligible, report := FilterAudience(contacts, freqCapped, "marketing")

	assert.Len(t, eligible, 2)
	assert.Equal(t, "a", eligible[0].ContactID)
	assert.Equal(t, "c", eligible[1].ContactID)
	assert.Equal(t, 1, report.FreqCap)
	assert.Equal(t, 1, report.Total)
}

func TestFilterAudience_NonMarketingSkipsUSAndFreqCap(t *testing.T) {
	contacts := []db.Contact{
		{ID: "a", WAPhone: "+12025551234", OptedIn: true}, // US — allowed for utility
		{ID: "b", WAPhone: "+919876543210", OptedIn: true},
	}
	freqCapped := map[string]bool{"a": true}

	eligible, report := FilterAudience(contacts, freqCapped, "utility")

	assert.Len(t, eligible, 2, "utility should not exclude US or freq-capped")
	assert.Equal(t, 0, report.Total)
}

func TestFilterAudience_MultipleExclusions(t *testing.T) {
	contacts := []db.Contact{
		{ID: "a", WAPhone: "+919876543210", OptedIn: true},              // eligible
		{ID: "b", WAPhone: "+12025551234", OptedIn: true},               // US excluded
		{ID: "c", WAPhone: "+919876543212", OptedIn: true},              // freq-capped
		{ID: "d", WAPhone: "+447911123456", OptedIn: true},              // eligible
	}
	freqCapped := map[string]bool{"c": true}

	eligible, report := FilterAudience(contacts, freqCapped, "marketing")

	assert.Len(t, eligible, 2)
	assert.Equal(t, 1, report.USNumber)
	assert.Equal(t, 1, report.FreqCap)
	assert.Equal(t, 2, report.Total)
}

// ── CalcCost ──────────────────────────────────────────────────────────────────

func TestCalcCost(t *testing.T) {
	rates := db.ConfigRates{Marketing: 0.8631, Utility: 0.115, Auth: 0.115, GSTRate: 0.18}

	t.Run("marketing with GST", func(t *testing.T) {
		got := CalcCost("marketing", rates)
		want := 0.8631 * 1.18
		assert.InDelta(t, want, got, 0.0001)
	})

	t.Run("utility with GST", func(t *testing.T) {
		got := CalcCost("utility", rates)
		want := 0.115 * 1.18
		assert.InDelta(t, want, got, 0.0001)
	})

	t.Run("authentication with GST", func(t *testing.T) {
		got := CalcCost("authentication", rates)
		want := 0.115 * 1.18
		assert.InDelta(t, want, got, 0.0001)
	})

	t.Run("service is free", func(t *testing.T) {
		got := CalcCost("service", rates)
		assert.Equal(t, 0.0, got)
	})
}

func TestCalcTotalCost(t *testing.T) {
	rates := db.ConfigRates{Marketing: 0.8631, Utility: 0.115, Auth: 0.115, GSTRate: 0.18}
	got := CalcTotalCost("marketing", 100, rates)
	want := 0.8631 * 1.18 * 100
	assert.InDelta(t, want, got, 0.001)
}
