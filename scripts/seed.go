package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/whatsapptool?sslmode=disable"
	}

	log.Printf("Connecting to database: %s", dbURL)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	// 1. Create Tenants
	log.Println("Seeding Tenants...")
	var tenant1ID, tenant2ID string

	// Acme Corp
	err = pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, status)
		VALUES ('Acme Corp', 'acme', 'active')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`).Scan(&tenant1ID)
	if err != nil {
		log.Fatalf("Failed to seed tenant Acme: %v", err)
	}
	log.Printf("Seeded Tenant: Acme Corp (ID: %s)", tenant1ID)

	// Globex LLC
	err = pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, status)
		VALUES ('Globex LLC', 'globex', 'active')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`).Scan(&tenant2ID)
	if err != nil {
		log.Fatalf("Failed to seed tenant Globex: %v", err)
	}
	log.Printf("Seeded Tenant: Globex LLC (ID: %s)", tenant2ID)

	// Suspended Tenant: Hooli
	var suspendedTenantID string
	err = pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, status)
		VALUES ('Hooli Inc', 'hooli', 'suspended')
		ON CONFLICT (slug) DO UPDATE SET status = EXCLUDED.status
		RETURNING id::text
	`).Scan(&suspendedTenantID)
	if err != nil {
		log.Fatalf("Failed to seed tenant Hooli: %v", err)
	}
	log.Printf("Seeded Suspended Tenant: Hooli Inc (ID: %s)", suspendedTenantID)

	// Helper function to hash password
	hashPassword := func(pw string) string {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}
		return string(hash)
	}
	pwdHash := hashPassword("password123")

	// 2. Create Agents (Users)
	log.Println("Seeding Agents...")
	
	// Acme Admin
	var acmeAdminID string
	err = pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role, tenant_id)
		VALUES ('Acme Admin', 'admin@acme.com', $1, 'admin', $2::uuid)
		ON CONFLICT (tenant_id, email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`, pwdHash, tenant1ID).Scan(&acmeAdminID)
	if err != nil {
		log.Fatalf("Failed to seed Acme Admin: %v", err)
	}
	log.Printf("Seeded Agent: admin@acme.com (Tenant: Acme)")

	// Globex Manager
	var globexMgrID string
	err = pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role, tenant_id)
		VALUES ('Globex Manager', 'manager@globex.com', $1, 'manager', $2::uuid)
		ON CONFLICT (tenant_id, email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`, pwdHash, tenant2ID).Scan(&globexMgrID)
	if err != nil {
		log.Fatalf("Failed to seed Globex Manager: %v", err)
	}
	log.Printf("Seeded Agent: manager@globex.com (Tenant: Globex)")

	// Super Admin
	var superAdminID string
	err = pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role, tenant_id)
		VALUES ('Super Admin', 'super@platform.com', $1, 'super_admin', '00000000-0000-0000-0000-000000000000'::uuid)
		ON CONFLICT (tenant_id, email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`, pwdHash).Scan(&superAdminID)
	if err != nil {
		log.Fatalf("Failed to seed Super Admin: %v", err)
	}
	log.Printf("Seeded Agent: super@platform.com (Platform Super Admin)")

	// 3. Seed Configs for Tenants
	log.Println("Seeding App Config Settings...")
	seedConfig := func(tenantID, key, val string) {
		_, err := pool.Exec(ctx, `
			INSERT INTO app_config (tenant_id, key, value)
			VALUES ($1::uuid, $2, to_jsonb($3::text))
			ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value
		`, tenantID, key, val)
		if err != nil {
			log.Fatalf("Failed to seed config key %s: %v", key, err)
		}
	}

	// Configs for Acme
	seedConfig(tenant1ID, "whatsapp_phone_number_id", "1234567890")
	seedConfig(tenant1ID, "whatsapp_waba_id", "acme-waba-id")
	seedConfig(tenant1ID, "whatsapp_access_token", "acme-access-token-1234")
	seedConfig(tenant1ID, "marketing_cost_inr", "0.72")
	seedConfig(tenant1ID, "utility_cost_inr", "0.35")
	seedConfig(tenant1ID, "auth_cost_inr", "0.15")
	seedConfig(tenant1ID, "quiet_hours_start_ist", "21:00")
	seedConfig(tenant1ID, "quiet_hours_end_ist", "09:00")
	seedConfig(tenant1ID, "gst_rate", "0.18")
	seedConfig(tenant1ID, "daily_message_cap", "250")

	// Configs for Globex
	seedConfig(tenant2ID, "whatsapp_phone_number_id", "9876543210")
	seedConfig(tenant2ID, "whatsapp_waba_id", "globex-waba-id")
	seedConfig(tenant2ID, "whatsapp_access_token", "globex-access-token-5678")
	seedConfig(tenant2ID, "marketing_cost_inr", "0.80")
	seedConfig(tenant2ID, "utility_cost_inr", "0.40")
	seedConfig(tenant2ID, "auth_cost_inr", "0.20")
	seedConfig(tenant2ID, "quiet_hours_start_ist", "22:00")
	seedConfig(tenant2ID, "quiet_hours_end_ist", "08:00")
	seedConfig(tenant2ID, "gst_rate", "0.18")
	seedConfig(tenant2ID, "daily_message_cap", "1000")

	// 4. Seed Tags and Contacts
	log.Println("Seeding Tags & Contacts...")
	var tag1ID, tag2ID int64

	// Tags for Acme
	err = pool.QueryRow(ctx, `
		INSERT INTO tags (name, color, tenant_id)
		VALUES ('VIP Customers', '#EF4444', $1::uuid)
		ON CONFLICT (tenant_id, name) DO UPDATE SET color = EXCLUDED.color
		RETURNING id
	`, tenant1ID).Scan(&tag1ID)
	if err != nil {
		log.Fatalf("Failed to seed tag VIP Customers: %v", err)
	}

	// Tags for Globex
	err = pool.QueryRow(ctx, `
		INSERT INTO tags (name, color, tenant_id)
		VALUES ('Newsletter Subscribers', '#10B981', $1::uuid)
		ON CONFLICT (tenant_id, name) DO UPDATE SET color = EXCLUDED.color
		RETURNING id
	`, tenant2ID).Scan(&tag2ID)
	if err != nil {
		log.Fatalf("Failed to seed tag Newsletter: %v", err)
	}

	// Acme Contacts
	_, err = pool.Exec(ctx, `
		INSERT INTO contacts (wa_phone, name, email, opted_in, opt_in_source, opt_in_at, tenant_id)
		VALUES 
			('+919999999999', 'Alice Johnson', 'alice@acme.com', true, 'manual', NOW(), $1::uuid),
			('+918888888888', 'Bob Smith', 'bob@acme.com', true, 'csv_import', NOW(), $1::uuid)
		ON CONFLICT (tenant_id, wa_phone) DO NOTHING
	`, tenant1ID)
	if err != nil {
		log.Fatalf("Failed to seed Acme contacts: %v", err)
	}

	// Globex Contacts
	_, err = pool.Exec(ctx, `
		INSERT INTO contacts (wa_phone, name, email, opted_in, opt_in_source, opt_in_at, tenant_id)
		VALUES 
			('+917777777777', 'Charlie Brown', 'charlie@globex.com', true, 'api', NOW(), $1::uuid),
			('+12025550143', 'US Customer', 'us@globex.com', true, 'manual', NOW(), $1::uuid)
		ON CONFLICT (tenant_id, wa_phone) DO NOTHING
	`, tenant2ID)
	if err != nil {
		log.Fatalf("Failed to seed Globex contacts: %v", err)
	}

	// Assign Tags to Contacts
	_, err = pool.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id)
		SELECT id, $1 FROM contacts WHERE tenant_id = $2::uuid
		ON CONFLICT DO NOTHING
	`, tag1ID, tenant1ID)
	if err != nil {
		log.Fatalf("Failed to link VIP Customers tag: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id)
		SELECT id, $1 FROM contacts WHERE tenant_id = $2::uuid
		ON CONFLICT DO NOTHING
	`, tag2ID, tenant2ID)
	if err != nil {
		log.Fatalf("Failed to link Newsletter tag: %v", err)
	}

	// 5. Seed Templates
	log.Println("Seeding Templates...")
	var template1ID, template2ID string
	
	// Acme Template
	err = pool.QueryRow(ctx, `
		INSERT INTO templates (name, language, category, status, components, tenant_id)
		VALUES (
			'welcome_acme', 'en', 'marketing', 'approved',
			'[{"type": "HEADER", "format": "TEXT", "text": "Welcome to Acme!"}, {"type": "BODY", "text": "Hi {{1}}, thank you for joining."}]'::jsonb,
			$1::uuid
		)
		ON CONFLICT (tenant_id, name, language) DO UPDATE SET status = EXCLUDED.status
		RETURNING id::text
	`, tenant1ID).Scan(&template1ID)
	if err != nil {
		log.Fatalf("Failed to seed Acme template: %v", err)
	}

	// Globex Template
	err = pool.QueryRow(ctx, `
		INSERT INTO templates (name, language, category, status, components, tenant_id)
		VALUES (
			'newsletter_update', 'en', 'marketing', 'approved',
			'[{"type": "BODY", "text": "Hello {{1}}, here is your daily newsletter."}]'::jsonb,
			$1::uuid
		)
		ON CONFLICT (tenant_id, name, language) DO UPDATE SET status = EXCLUDED.status
		RETURNING id::text
	`, tenant2ID).Scan(&template2ID)
	if err != nil {
		log.Fatalf("Failed to seed Globex template: %v", err)
	}

	// 6. Seed Campaigns
	log.Println("Seeding Campaigns...")
	_, err = pool.Exec(ctx, `
		INSERT INTO campaigns (name, template_id, template_variables, segment_tags, status, tenant_id, total_recipients)
		VALUES (
			'Acme Summer Sale', $1::uuid, '{"1": "name"}', '[]', 'completed', $2::uuid, 2
		)
		ON CONFLICT DO NOTHING
	`, template1ID, tenant1ID)
	if err != nil {
		log.Fatalf("Failed to seed Acme campaign: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO campaigns (name, template_id, template_variables, segment_tags, status, tenant_id, total_recipients)
		VALUES (
			'Globex Weekly Roundup', $1::uuid, '{"1": "name"}', '[]', 'draft', $2::uuid, 2
		)
		ON CONFLICT DO NOTHING
	`, template2ID, tenant2ID)
	if err != nil {
		log.Fatalf("Failed to seed Globex campaign: %v", err)
	}

	log.Println("Seeding complete! Platform is populated with multiple tenants, users, and dummy data.")
	log.Println("Credentials for Login:")
	log.Printf("- Platform Super Admin: super@platform.com (password: password123)")
	log.Printf("- Acme Admin: admin@acme.com (password: password123)")
	log.Printf("- Globex Manager: manager@globex.com (password: password123)")
}
