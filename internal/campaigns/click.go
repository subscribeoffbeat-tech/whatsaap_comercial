package campaigns

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WrapURL creates a click-tracking row and returns the /c/{short_code} redirect URL.
// baseURL should be e.g. "https://example.com" (no trailing slash).
// messageID may be empty string if the message hasn't been sent yet.
func WrapURL(
	ctx context.Context,
	pool *pgxpool.Pool,
	campaignID, contactID, messageID, originalURL, baseURL string,
) (string, error) {
	code, err := newShortCode()
	if err != nil {
		return "", fmt.Errorf("click wrap: generate short code: %w", err)
	}

	var msgID *string
	if messageID != "" {
		msgID = &messageID
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO click_tracking (short_code, original_url, campaign_id, contact_id, message_id)
		VALUES ($1, $2, $3::uuid, $4::uuid, $5::uuid)
	`, code, originalURL, campaignID, contactID, msgID)
	if err != nil {
		return "", fmt.Errorf("click wrap: insert: %w", err)
	}
	return baseURL + "/c/" + code, nil
}

// RecordClick increments the click counter and returns the original URL.
// Returns ("", ErrNotFound) if the short_code is unknown.
func RecordClick(ctx context.Context, pool *pgxpool.Pool, shortCode string) (string, error) {
	var url string
	err := pool.QueryRow(ctx, `
		UPDATE click_tracking
		SET click_count     = click_count + 1,
		    last_clicked_at = NOW(),
		    first_clicked_at = COALESCE(first_clicked_at, NOW())
		WHERE short_code = $1
		RETURNING original_url
	`, shortCode).Scan(&url)
	if err != nil {
		return "", err
	}
	return url, nil
}

// newShortCode generates an 8-character URL-safe random token.
func newShortCode() (string, error) {
	b := make([]byte, 6) // 6 raw bytes → 8 base64url chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
