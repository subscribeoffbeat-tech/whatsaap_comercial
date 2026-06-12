package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a pool connected to TEST_DATABASE_URL, or skips the test.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAuditLog(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var agentID string
	err := pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role)
		VALUES ('Audit Tester', 'audit@test.com', 'x', 'admin')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`).Scan(&agentID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM audit_log WHERE actor_id = $1::uuid`, agentID)
		pool.Exec(ctx, `DELETE FROM agents WHERE id = $1::uuid`, agentID)
	})

	// Log is fire-and-forget — no panic expected.
	Log(ctx, pool, agentID, "test_action", "contact", "contact-123",
		map[string]any{"note": "created via test"})

	// Give the async write a moment.
	time.Sleep(100 * time.Millisecond)

	entries, err := ListAuditLog(ctx, pool, "test_action", 10, 0)
	if err != nil {
		t.Fatalf("list audit log: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Action == "test_action" && e.ActorName == "Audit Tester" {
			found = true
			if e.EntityType == nil || *e.EntityType != "contact" {
				t.Errorf("want entity_type='contact', got %v", e.EntityType)
			}
			if e.EntityID == nil || *e.EntityID != "contact-123" {
				t.Errorf("want entity_id='contact-123', got %v", e.EntityID)
			}
		}
	}
	if !found {
		t.Error("audit entry not found in ListAuditLog result")
	}
}

func TestAuditLogFilterByAction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var agentID string
	err := pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role)
		VALUES ('Filter Tester', 'filter@test.com', 'x', 'admin')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`).Scan(&agentID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM audit_log WHERE actor_id = $1::uuid`, agentID)
		pool.Exec(ctx, `DELETE FROM agents WHERE id = $1::uuid`, agentID)
	})

	Log(ctx, pool, agentID, "action_alpha", "config", "x", nil)
	Log(ctx, pool, agentID, "action_beta",  "config", "y", nil)
	time.Sleep(100 * time.Millisecond)

	// Filter by specific action — should not return action_beta.
	entries, err := ListAuditLog(ctx, pool, "action_alpha", 20, 0)
	if err != nil {
		t.Fatalf("list audit log: %v", err)
	}
	for _, e := range entries {
		if e.Action == "action_beta" {
			t.Error("action_beta should not appear when filtering for action_alpha")
		}
	}

	// Empty filter — should return both.
	all, err := ListAuditLog(ctx, pool, "", 100, 0)
	if err != nil {
		t.Fatalf("list all audit log: %v", err)
	}
	alpha, beta := false, false
	for _, e := range all {
		if e.Action == "action_alpha" { alpha = true }
		if e.Action == "action_beta"  { beta = true }
	}
	if !alpha || !beta {
		t.Errorf("expected both actions in unfiltered list: alpha=%v beta=%v", alpha, beta)
	}
}
