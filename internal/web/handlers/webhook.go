package handlers

import (
	"io"
	"log"
	"net/http"

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
		// Cap the body before buffering: the HMAC can only be checked after the
		// full body is read, so without a limit an oversized POST to /webhook is a
		// pre-auth memory-exhaustion vector. Meta payloads are small (a few MB is
		// generous).
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20) // 4 MiB
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("webhook: body read/oversize from %s: %v", r.RemoteAddr, err)
			http.Error(w, "bad request", http.StatusBadRequest)
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

		// Handle template approval/rejection from Meta. Normalize Meta's status
		// enum to our local values so the gallery badges/pills always match;
		// skip events we don't recognize rather than writing a junk status.
		for _, tsu := range result.TemplateStatusUpdates {
			status := mapMetaStatus(tsu.Event)
			if status == "" {
				log.Printf("template status: ignoring unrecognized Meta event %q for %s", tsu.Event, tsu.Name)
				continue
			}
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

		// Record phone-number quality changes so ops can watch quality drops.
		if result.PhoneQualityEvent != "" {
			log.Printf("phone quality update: event=%s tier=%s", result.PhoneQualityEvent, result.PhoneCurrentLimit)
			if err := db.SetConfig(r.Context(), pool, "phone_quality_event", result.PhoneQualityEvent); err != nil {
				log.Printf("set phone_quality_event: %v", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}
