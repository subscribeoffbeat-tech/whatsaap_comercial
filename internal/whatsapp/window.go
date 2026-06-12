package whatsapp

import (
	"fmt"
	"time"
)

// WindowDuration is the Meta 24-hour free-form messaging window.
const WindowDuration = 24 * time.Hour

// IsWindowOpen returns true if the 24h free-form reply window is still open.
// The window opens when a contact sends any inbound message and closes 24h later.
// Outside the window, only approved template messages may be sent.
func IsWindowOpen(lastInboundAt *time.Time) bool {
	if lastInboundAt == nil {
		return false
	}
	return time.Now().UTC().Before(lastInboundAt.UTC().Add(WindowDuration))
}

// WindowExpiresAt returns when the 24h window closes for the given last-inbound time.
func WindowExpiresAt(lastInboundAt time.Time) time.Time {
	return lastInboundAt.UTC().Add(WindowDuration)
}

// TimeUntilExpiry returns how long remains in the window.
// Returns 0 if the window is already closed or if lastInboundAt is nil.
func TimeUntilExpiry(lastInboundAt *time.Time) time.Duration {
	if lastInboundAt == nil {
		return 0
	}
	remaining := time.Until(lastInboundAt.UTC().Add(WindowDuration))
	if remaining < 0 {
		return 0
	}
	return remaining
}

// FormatExpiry returns a human-readable "Xh Ym" string for the UI header badge.
// Returns "" if the window is closed.
func FormatExpiry(lastInboundAt *time.Time) string {
	d := TimeUntilExpiry(lastInboundAt)
	if d == 0 {
		return ""
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}
