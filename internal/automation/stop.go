package automation

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	"whatsapptool/internal/whatsapp"
)

var stopKeywords = []string{
	"STOP", "UNSUBSCRIBE", "OPT OUT", "OPTOUT", "OPT-OUT", "CANCEL",
}

// IsStopKeyword returns true if the trimmed, uppercased text exactly matches
// any recognised opt-out keyword.
func IsStopKeyword(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	for _, kw := range stopKeywords {
		if upper == kw {
			return true
		}
	}
	return false
}

const stopConfirmMessage = "You have been unsubscribed and will no longer receive " +
	"marketing messages from us. Reply START to opt back in."

// HandleStop opts the contact out in the DB and sends the standard confirmation
// message. It is safe to call in a goroutine.
func HandleStop(ctx context.Context, pool *pgxpool.Pool, client *whatsapp.Client, phone string) error {
	if err := db.OptOut(ctx, pool, phone); err != nil {
		return err
	}
	_, err := client.SendText(ctx, phone, stopConfirmMessage)
	return err
}
