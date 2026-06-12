package whatsapp_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"whatsapptool/internal/whatsapp"
)

// ── helpers ───────────────────────────────────────────────────────────────

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func buildPayload(msgs []map[string]any, statuses []map[string]any) []byte {
	p := map[string]any{
		"object": "whatsapp_business_account",
		"entry": []map[string]any{{
			"id": "WABAID",
			"changes": []map[string]any{{
				"field": "messages",
				"value": map[string]any{
					"messaging_product": "whatsapp",
					"messages":          msgs,
					"statuses":          statuses,
				},
			}},
		}},
	}
	b, _ := json.Marshal(p)
	return b
}

// ── VerifySignature ───────────────────────────────────────────────────────

func TestVerifySignature(t *testing.T) {
	const secret = "test_app_secret"
	body := []byte(`{"object":"whatsapp_business_account"}`)

	tests := []struct {
		name string
		sig  string
		body []byte
		want bool
	}{
		{name: "valid signature", sig: sign(secret, string(body)), body: body, want: true},
		{name: "wrong secret", sig: sign("different_secret", string(body)), body: body, want: false},
		{name: "tampered body", sig: sign(secret, string(body)), body: []byte(`{"object":"tampered"}`), want: false},
		{
			name: "missing sha256= prefix",
			sig:  hex.EncodeToString(func() []byte { m := hmac.New(sha256.New, []byte(secret)); m.Write(body); return m.Sum(nil) }()),
			body: body,
			want: false,
		},
		{name: "empty signature", sig: "", body: body, want: false},
		{name: "invalid hex after prefix", sig: "sha256=notvalidhex!!!", body: body, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := whatsapp.VerifySignature([]byte(secret), tt.body, tt.sig)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── ExtractInboundIDs ─────────────────────────────────────────────────────

func TestExtractInboundIDs(t *testing.T) {
	t.Run("single inbound message", func(t *testing.T) {
		b := buildPayload(
			[]map[string]any{{"id": "wamid.1", "from": "911234567890", "type": "text"}},
			nil,
		)
		ids, err := whatsapp.ExtractInboundIDs(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 1 || ids[0] != "wamid.1" {
			t.Errorf("got %v, want [wamid.1]", ids)
		}
	})

	t.Run("status-only payload returns empty (statuses not deduped)", func(t *testing.T) {
		b := buildPayload(
			nil,
			[]map[string]any{{"id": "wamid.2", "status": "delivered", "recipient_id": "911234567890"}},
		)
		ids, err := whatsapp.ExtractInboundIDs(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected no ids for status-only payload, got %v", ids)
		}
	})

	t.Run("combined payload: only message IDs extracted", func(t *testing.T) {
		b := buildPayload(
			[]map[string]any{{"id": "wamid.A", "from": "91111", "type": "text"}},
			[]map[string]any{{"id": "wamid.B", "status": "read", "recipient_id": "91111"}},
		)
		ids, err := whatsapp.ExtractInboundIDs(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 1 || ids[0] != "wamid.A" {
			t.Errorf("got %v, want [wamid.A] (status ID must not be extracted)", ids)
		}
	})

	t.Run("empty payload returns nil", func(t *testing.T) {
		b := buildPayload(nil, nil)
		ids, err := whatsapp.ExtractInboundIDs(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected empty, got %v", ids)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := whatsapp.ExtractInboundIDs([]byte("not json {"))
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})
}

// ── Processor / ProcessResult ─────────────────────────────────────────────

type mockStore struct {
	logs   map[int64][]byte
	seen   map[string]int64
	nextID int64
}

func newMockStore() *mockStore {
	return &mockStore{logs: make(map[int64][]byte), seen: make(map[string]int64)}
}

func (m *mockStore) LogWebhook(_ context.Context, payload []byte) (int64, error) {
	m.nextID++
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.logs[m.nextID] = cp
	return m.nextID, nil
}

func (m *mockStore) RecordMessageIDs(_ context.Context, logID int64, ids []string) ([]string, error) {
	var fresh []string
	for _, id := range ids {
		if _, exists := m.seen[id]; !exists {
			m.seen[id] = logID
			fresh = append(fresh, id)
		}
	}
	return fresh, nil
}

func TestProcessor_Deduplication(t *testing.T) {
	msgPayload := func(msgID string) []byte {
		return buildPayload(
			[]map[string]any{{"id": msgID, "from": "911234567890", "type": "text",
				"text": map[string]any{"body": "hello"}}},
			nil,
		)
	}

	store := newMockStore()
	proc := whatsapp.NewProcessor(store)
	ctx := context.Background()

	t.Run("first delivery: message in NewInboundMessages", func(t *testing.T) {
		res, err := proc.ProcessRaw(ctx, msgPayload("wamid.abc"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.NewInboundMessages) != 1 || res.NewInboundMessages[0].ID != "wamid.abc" {
			t.Errorf("got NewInboundMessages=%v, want one message wamid.abc", res.NewInboundMessages)
		}
	})

	t.Run("retry of same message: NewInboundMessages empty", func(t *testing.T) {
		res, err := proc.ProcessRaw(ctx, msgPayload("wamid.abc"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.NewInboundMessages) != 0 {
			t.Errorf("duplicate must not appear in NewInboundMessages, got %v", res.NewInboundMessages)
		}
	})

	t.Run("different message ID is new", func(t *testing.T) {
		res, err := proc.ProcessRaw(ctx, msgPayload("wamid.xyz"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.NewInboundMessages) != 1 || res.NewInboundMessages[0].ID != "wamid.xyz" {
			t.Errorf("got %v, want [wamid.xyz]", res.NewInboundMessages)
		}
	})

	t.Run("webhook is always logged, even for duplicates", func(t *testing.T) {
		before := len(store.logs)
		_, err := proc.ProcessRaw(ctx, msgPayload("wamid.abc")) // duplicate
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(store.logs) != before+1 {
			t.Errorf("every webhook must be logged; before=%d after=%d", before, len(store.logs))
		}
	})

	t.Run("no-message payload: empty result, still logged", func(t *testing.T) {
		before := len(store.logs)
		res, err := proc.ProcessRaw(ctx, buildPayload(nil, nil))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.NewInboundMessages) != 0 {
			t.Errorf("expected empty NewInboundMessages, got %v", res.NewInboundMessages)
		}
		if len(store.logs) != before+1 {
			t.Error("expected webhook to be logged even with no messages")
		}
	})
}

func TestProcessor_StatusUpdates(t *testing.T) {
	store := newMockStore()
	proc := whatsapp.NewProcessor(store)
	ctx := context.Background()

	statusPayload := buildPayload(
		nil,
		[]map[string]any{
			{"id": "wamid.out1", "status": "delivered", "recipient_id": "911234567890"},
		},
	)

	t.Run("status updates always returned regardless of prior processing", func(t *testing.T) {
		// Process same status twice (simulating Meta retry)
		res1, err := proc.ProcessRaw(ctx, statusPayload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res1.StatusUpdates) != 1 || res1.StatusUpdates[0].ID != "wamid.out1" {
			t.Errorf("first: got %v, want status for wamid.out1", res1.StatusUpdates)
		}

		// Second delivery: status update still returned (idempotent — must not be swallowed)
		res2, err := proc.ProcessRaw(ctx, statusPayload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res2.StatusUpdates) != 1 {
			t.Errorf("second: status update must still be returned on retry, got %v", res2.StatusUpdates)
		}
	})

	t.Run("combined payload: inbound deduped, statuses always present", func(t *testing.T) {
		combined := buildPayload(
			[]map[string]any{{"id": "wamid.new", "from": "91999", "type": "text"}},
			[]map[string]any{{"id": "wamid.old_out", "status": "read", "recipient_id": "91999"}},
		)
		res, err := proc.ProcessRaw(ctx, combined)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.NewInboundMessages) != 1 {
			t.Errorf("expected 1 new message, got %v", res.NewInboundMessages)
		}
		if len(res.StatusUpdates) != 1 {
			t.Errorf("expected 1 status update, got %v", res.StatusUpdates)
		}
	})
}
