package whatsapp_test

import (
	"testing"
	"time"

	"whatsapptool/internal/whatsapp"
)

func ptr(t time.Time) *time.Time { return &t }

// ── IsWindowOpen ──────────────────────────────────────────────────────────

func TestIsWindowOpen(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name          string
		lastInboundAt *time.Time
		want          bool
	}{
		{
			name:          "nil — no inbound ever",
			lastInboundAt: nil,
			want:          false,
		},
		{
			name:          "message 1 second ago — window open",
			lastInboundAt: ptr(now.Add(-1 * time.Second)),
			want:          true,
		},
		{
			name:          "message 12 hours ago — window open",
			lastInboundAt: ptr(now.Add(-12 * time.Hour)),
			want:          true,
		},
		{
			name:          "message exactly 24h ago — window closed",
			lastInboundAt: ptr(now.Add(-24 * time.Hour)),
			want:          false,
		},
		{
			name:          "message 24h+1s ago — window closed",
			lastInboundAt: ptr(now.Add(-24*time.Hour - time.Second)),
			want:          false,
		},
		{
			name:          "message 48 hours ago — window closed",
			lastInboundAt: ptr(now.Add(-48 * time.Hour)),
			want:          false,
		},
		{
			name:          "message in the future (clock skew) — window open",
			lastInboundAt: ptr(now.Add(1 * time.Second)),
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := whatsapp.IsWindowOpen(tt.lastInboundAt)
			if got != tt.want {
				t.Errorf("IsWindowOpen() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── TimeUntilExpiry ───────────────────────────────────────────────────────

func TestTimeUntilExpiry(t *testing.T) {
	now := time.Now().UTC()

	t.Run("nil returns 0", func(t *testing.T) {
		if d := whatsapp.TimeUntilExpiry(nil); d != 0 {
			t.Errorf("got %v, want 0", d)
		}
	})

	t.Run("12 hours ago — approx 12h remaining", func(t *testing.T) {
		ts := ptr(now.Add(-12 * time.Hour))
		d := whatsapp.TimeUntilExpiry(ts)
		// Allow ±5s tolerance for test execution time
		want := 12 * time.Hour
		delta := want - d
		if delta < 0 {
			delta = -delta
		}
		if delta > 5*time.Second {
			t.Errorf("got %v, want ~%v (within 5s)", d, want)
		}
	})

	t.Run("25 hours ago — returns 0, not negative", func(t *testing.T) {
		ts := ptr(now.Add(-25 * time.Hour))
		if d := whatsapp.TimeUntilExpiry(ts); d != 0 {
			t.Errorf("got %v, want 0 (closed window must not be negative)", d)
		}
	})
}

// ── WindowExpiresAt ───────────────────────────────────────────────────────

func TestWindowExpiresAt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	got := whatsapp.WindowExpiresAt(now)
	want := now.Add(24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("WindowExpiresAt(%v) = %v, want %v", now, got, want)
	}
}

// ── FormatExpiry ──────────────────────────────────────────────────────────

func TestFormatExpiry(t *testing.T) {
	now := time.Now().UTC()

	t.Run("nil returns empty string", func(t *testing.T) {
		if s := whatsapp.FormatExpiry(nil); s != "" {
			t.Errorf("got %q, want empty", s)
		}
	})

	t.Run("closed window returns empty string", func(t *testing.T) {
		ts := ptr(now.Add(-25 * time.Hour))
		if s := whatsapp.FormatExpiry(ts); s != "" {
			t.Errorf("got %q, want empty", s)
		}
	})

	t.Run("open window returns non-empty string", func(t *testing.T) {
		ts := ptr(now.Add(-1 * time.Hour))
		s := whatsapp.FormatExpiry(ts)
		if s == "" {
			t.Error("expected non-empty expiry string, got empty")
		}
	})

	t.Run("22h 30m remaining returns correct format", func(t *testing.T) {
		// Set last inbound to 1.5h ago → ~22h 30m remaining
		ts := ptr(now.Add(-90 * time.Minute))
		s := whatsapp.FormatExpiry(ts)
		if s == "" {
			t.Error("expected non-empty string")
		}
		// Should contain 'h' and 'm'
		if len(s) < 4 {
			t.Errorf("format too short: %q", s)
		}
	})
}

// ── Window edge: inbound resets the clock ────────────────────────────────

// TestWindowReset verifies that a newer inbound message extends the window.
func TestWindowReset(t *testing.T) {
	now := time.Now().UTC()

	// Old message 23h ago — window still open but about to close
	old := ptr(now.Add(-23 * time.Hour))
	if !whatsapp.IsWindowOpen(old) {
		t.Fatal("expected window open with 23h-old message")
	}
	if whatsapp.TimeUntilExpiry(old) > time.Hour+time.Minute {
		t.Fatal("expected less than 1h remaining")
	}

	// New message just now — window fully reset to 24h
	fresh := ptr(now)
	if !whatsapp.IsWindowOpen(fresh) {
		t.Fatal("expected window open after fresh message")
	}
	remaining := whatsapp.TimeUntilExpiry(fresh)
	if remaining < 23*time.Hour {
		t.Errorf("expected ~24h remaining after reset, got %v", remaining)
	}
}
