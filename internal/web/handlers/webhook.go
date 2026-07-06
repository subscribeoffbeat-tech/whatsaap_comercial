package handlers

import (
	"encoding/json"
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
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode == "subscribe" && token == verifyToken {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}

// WebhookReceive handles POST webhook events from Meta.
// Flow: read body → verify HMAC-SHA256 → extract tenant → dedupe → dispatch.
func WebhookReceive(
	proc *whatsapp.Processor,
	inbox *InboxHandler,
	pool *pgxpool.Pool,
	appSecret []byte,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		// Extract phone_number_id from payload and lookup corresponding tenant
		phoneID := extractPhoneNumberID(body)
		tenantID := ""
		if phoneID != "" {
			var err error
			tenantID, err = db.LookupTenantByPhoneID(r.Context(), pool, phoneID)
			if err != nil {
				log.Printf("webhook: unknown phone_number_id %q: %v", phoneID, err)
				// Return 200 so Meta doesn't retry
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		ctx := r.Context()
		if tenantID != "" {
			ctx = db.ContextWithTenant(ctx, tenantID)
		}

		result, err := proc.ProcessRaw(ctx, body)
		if err != nil {
			// Return 200 so Meta doesn't disable the webhook on persistent errors.
			log.Printf("webhook process: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Dispatch new inbound messages to the inbox layer.
		for _, msg := range result.NewInboundMessages {
			inbox.HandleInbound(ctx, msg)
		}

		// Apply all status updates (idempotent).
		for _, status := range result.StatusUpdates {
			inbox.HandleStatusUpdate(ctx, status)
		}

		// Handle template approval/rejection from Meta.
		for _, tsu := range result.TemplateStatusUpdates {
			status := mapMetaWebhookStatus(tsu.Event)
			if status == "" {
				log.Printf("template status: ignoring unrecognized Meta event %q for %s", tsu.Event, tsu.Name)
				continue
			}
			if err := db.SetTemplateStatus(ctx, pool, tsu.Name, tsu.Language, status, tsu.Reason); err != nil {
				log.Printf("set template status %s/%s → %s: %v", tsu.Name, tsu.Language, status, err)
			}
		}

		// Persist updated daily tier cap from Meta capability webhook.
		if result.NewDailyCap != nil {
			if err := db.SetConfig(ctx, pool, tenantID, "daily_message_cap", *result.NewDailyCap); err != nil {
				log.Printf("set daily_message_cap %d: %v", *result.NewDailyCap, err)
			}
		}

		// Record phone-number quality changes.
		if result.PhoneQualityEvent != "" {
			log.Printf("phone quality update: event=%s tier=%s", result.PhoneQualityEvent, result.PhoneCurrentLimit)
			if err := db.SetConfig(ctx, pool, tenantID, "phone_quality_event", result.PhoneQualityEvent); err != nil {
				log.Printf("set phone_quality_event: %v", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

// extractPhoneNumberID resolves the metadata.phone_number_id from the JSON payload.
func extractPhoneNumberID(payload []byte) string {
	var wp struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Metadata struct {
						PhoneNumberID string `json:"phone_number_id"`
					} `json:"metadata"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(payload, &wp); err == nil {
		for _, entry := range wp.Entry {
			for _, change := range entry.Changes {
				if change.Value.Metadata.PhoneNumberID != "" {
					return change.Value.Metadata.PhoneNumberID
				}
			}
		}
	}
	return ""
}

func mapMetaWebhookStatus(metaEvent string) string {
	switch strings.ToUpper(metaEvent) {
	case "APPROVED":
		return "approved"
	case "REJECTED":
		return "rejected"
	case "PENDING_DELETION":
		return "paused"
	case "FLAGGED":
		return "approved"
	}
	return ""
}
