package handlers

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	"whatsapptool/internal/whatsapp"
)

// WebhookVerify handles the GET challenge Meta sends during webhook registration.
func WebhookVerify(verifyToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode      := r.URL.Query().Get("hub.mode")
		token     := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode == "subscribe" && token == verifyToken {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}

// WebhookReceive handles POST webhook events from Meta.
// Flow: read body → verify HMAC-SHA256 → dedupe → dispatch to inbox + templates.
func WebhookReceive(
	proc      *whatsapp.Processor,
	inbox     *InboxHandler,
	pool      *pgxpool.Pool,
	appSecret []byte,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}

		if !whatsapp.VerifySignature(appSecret, body, r.Header.Get("X-Hub-Signature-256")) {
			log.Printf("webhook: signature verification failed from %s", r.RemoteAddr)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		result, err := proc.ProcessRaw(r.Context(), body)
		if err != nil {
			// Return 200 so Meta doesn't disable the webhook on persistent errors.
			log.Printf("webhook process: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Dispatch new inbound messages to the inbox layer.
		for _, msg := range result.NewInboundMessages {
			inbox.HandleInbound(r.Context(), msg)
		}

		// Apply all status updates (idempotent).
		for _, status := range result.StatusUpdates {
			inbox.HandleStatusUpdate(r.Context(), status)
		}

		// Handle template approval/rejection from Meta.
		for _, tsu := range result.TemplateStatusUpdates {
			status := strings.ToLower(tsu.Event)
			if err := db.SetTemplateStatus(r.Context(), pool, tsu.Name, tsu.Language, status, tsu.Reason); err != nil {
				log.Printf("set template status %s/%s → %s: %v", tsu.Name, tsu.Language, status, err)
			}
		}

		// Persist updated daily tier cap from Meta capability webhook.
		if result.NewDailyCap != nil {
			if err := db.SetConfig(r.Context(), pool, "daily_message_cap", *result.NewDailyCap); err != nil {
				log.Printf("set daily_message_cap %d: %v", *result.NewDailyCap, err)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}
