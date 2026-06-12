# WhatsApp Tool — build instructions for Claude Code

## What this is
Single-company (single-tenant) WhatsApp marketing + support tool for a SERVICE business.
Talks DIRECTLY to Meta Cloud API (v23.x) — no BSP. Self-hosted on one VPS.

## Stack (locked)
Go (chi router) + PostgreSQL (sqlc) + River queue (in Postgres) + templ + HTMX + Alpine.js.
Single binary. Nginx + certbot in front for HTTPS. WebSocket/SSE for the live inbox. JWT sessions.

## Project layout
cmd/server/ — entry point
internal/config/ — env loading, settings
internal/db/ — sqlc queries, migrations
internal/web/ — HTTP handlers, templates, static
internal/whatsapp/ — Meta API client, webhook handler
internal/campaigns/ — campaign builder, queue worker
internal/automation/ — keyword rules, welcome, away, STOP
migrations/ — SQL migration files
static/ — CSS, JS, images
docs/ — planning docs and screen mockups

## Scope — IN (v1)
- Broadcasts to tagged segments with paced sending
- Contacts with tags, custom fields (JSONB), notes
- Shared multi-agent inbox on one number (live via WebSocket)
- Templates + Meta approval tracking via webhook
- Single-step automation (keyword match, welcome, away message, STOP)
- Scheduling with quiet hours (9pm-9am IST)
- Click tracking (CTA link wrapping) + saved "clicked campaign X" segments
- Analytics + per-message cost tracking (stamp category + cost on every outbound)
- Consent: opt-in with source/timestamp proof, auto opt-out on STOP
- RBAC: Admin > Manager > Agent (see matrix below)
- Audit log for sends and setting changes

## Scope — OUT (do NOT build these)
Catalog, in-app payments, AI/NLP chatbot, visual drag-drop flow builder,
AI template generator, click-to-WhatsApp ads manager, WhatsApp Flows/forms,
native Shopify/Zapier/HubSpot integrations.
Keep ONE simple inbound API endpoint so external systems can push contacts/events.

## RBAC matrix
Admin: everything
Manager: chat, campaigns, contacts, templates, automation, analytics (full), NO team/secrets
Agent: chat (assigned + unassigned queue), tag/note in chat, own stats only

## Hard rules (MUST enforce in code)
- Daily cap: stop queuing at 90% of current tier (read tier from business_capability_update webhook)
- Throughput: token-bucket rate limiter at ~80 msg/sec, raise only when tier allows
- Retries: 3 attempts, exponential backoff, transient errors only; 131049/invalid number = fail permanently
- Frequency cap: max 1 marketing message per contact per 24 hours
- Quiet hours: 9pm-9am IST for business-initiated sends; service replies always allowed
- Consent hard block: non-opted-in contacts CANNOT enter a campaign
- STOP/UNSUBSCRIBE keyword = instant opt-out + auto-confirm message
- Exclude +1 (US) numbers from marketing campaigns (blocked since Apr 2025)
- 24h window per conversation: free-form replies inside, template-only outside
- Webhook safety: verify X-Hub-Signature-256 -> store raw payload to webhook_log -> dedupe by wa_message_id -> download media immediately (URLs expire)
- Token: encrypted at rest, loaded from env, NEVER committed to git
- Data retention: purge conversations/messages after 24 months (configurable); honor delete-on-request

## Cost rates (store in config table, NOT hard-coded — rates drift)
Marketing: Rs 0.8631/msg | Utility: Rs 0.115/msg | Auth: Rs 0.115/msg | Service: free
+18% GST on all Meta charges. Stamp cost on every outbound message row.

## Chat routing
Default: round-robin to available agents.
Override: tag-based routing (e.g. "billing" tag -> billing agent group).
Fallback: unassigned queue (any agent can claim). Managers can reassign.

## Template variables
Pulled from contact fields (name + custom JSONB keys). Every variable MUST have a fallback default.

## Build order
1. Foundation: config loading, Postgres schema + migrations, webhook receiver (verify sig -> raw log -> dedupe)
2. Messaging core: send/receive via Meta API, 24h-window logic, shared inbox with live WebSocket
3. Contacts + templates: import CSV + tags + consent stamping + STOP handler, template CRUD + submit + status tracking
4. Campaigns: builder wizard, River queue, paced worker, limit guard, scheduling, quiet hours, US exclusion
5. Automation + insight: keyword/welcome/away rules, click tracking (link wrapping), analytics + cost rollups, quality watch
6. Hardening: RBAC enforcement, audit log, settings page, backup script, /health endpoint, ops runbook

## How to work
- Read /docs before building (the full plan + screen mockups are there)
- Build in SMALL increments — one feature, then tests, then commit
- Write tests alongside every feature (especially: webhook dedupe, 24h-window logic, rate limiter, opt-out)
- Do NOT over-engineer or add microservices — this is a single modular monolith (one binary)
- If unsure whether something is in scope, check the OUT list above — if it is there, do not build it
