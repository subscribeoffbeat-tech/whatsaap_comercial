package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/campaigns"
	"whatsapptool/internal/db"
	"whatsapptool/internal/whatsapp"
)

// RunRules evaluates active automation rules against an inbound message.
// It is called after the STOP check (which is handled in inbox.go).
// Returns true if any rule fired (caller may suppress further processing).
//
// isNewContact should be true when this is the contact's very first message.
func RunRules(
	ctx context.Context,
	pool *pgxpool.Pool,
	waClient *whatsapp.Client,
	msg whatsapp.InboundMessage,
	contactID, convID string,
	isNewContact bool,
) (bool, error) {
	rules, err := db.ListActiveRules(ctx, pool)
	if err != nil {
		return false, err
	}

	phone := msg.From
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	var text string
	if msg.Text != nil {
		text = strings.TrimSpace(msg.Text.Body)
	}

	now := time.Now()
	inQuiet := campaigns.IsQuietHoursCfg(ctx, pool, now)

	for _, rule := range rules {
		if !matchesRule(rule, text, isNewContact, now, inQuiet) {
			continue
		}

		fired, err := executeRule(ctx, pool, waClient, rule, contactID, phone, convID)
		if err != nil {
			log.Printf("automation rule %d (%s) execute: %v", rule.ID, rule.Name, err)
			continue
		}
		if fired {
			return true, nil // first match wins
		}
	}
	return false, nil
}

// matchesRule returns true if the rule's trigger conditions are met.
func matchesRule(rule *db.AutomationRule, text string, isNewContact bool, now time.Time, inQuiet bool) bool {
	switch rule.TriggerType {
	case "stop":
		// Handled separately in the inbox handler; never fire here.
		return false

	case "keyword":
		if rule.Keyword == nil || *rule.Keyword == "" {
			return false
		}
		kw := *rule.Keyword
		switch rule.KeywordMatch {
		case "exact":
			return strings.EqualFold(text, kw)
		default: // "contains"
			return strings.Contains(strings.ToLower(text), strings.ToLower(kw))
		}

	case "welcome":
		return isNewContact

	case "away":
		return inQuiet
	}
	return false
}

// executeRule performs the rule's action and returns true if it did something.
// It branches on ActionType so each action does its real job (e.g. add_tag adds
// a tag rather than texting the tag name).
func executeRule(
	ctx context.Context,
	pool *pgxpool.Pool,
	waClient *whatsapp.Client,
	rule *db.AutomationRule,
	contactID, phone, convID string,
) (bool, error) {
	switch rule.ActionType {
	case "send_template":
		return sendTemplateAction(ctx, pool, waClient, rule, phone, convID)

	case "add_tag":
		name := ""
		if rule.ResponseText != nil {
			name = strings.TrimSpace(*rule.ResponseText)
		}
		if name == "" {
			return false, nil
		}
		tagID, err := getOrCreateTagID(ctx, pool, name)
		if err != nil {
			return false, err
		}
		if err := db.AddContactTag(ctx, pool, contactID, tagID); err != nil {
			return false, err
		}
		return true, nil

	case "assign_agent":
		if _, err := db.AssignRoundRobin(ctx, pool, convID); err != nil {
			return false, err
		}
		return true, nil

	case "remove_consent":
		if err := db.OptOut(ctx, pool, phone); err != nil {
			return false, err
		}
		return true, nil

	case "webhook":
		if rule.ResponseText == nil || strings.TrimSpace(*rule.ResponseText) == "" {
			return false, nil
		}
		return fireWebhook(ctx, strings.TrimSpace(*rule.ResponseText), rule, contactID, phone, convID), nil

	default:
		// Legacy rules (no action_type): text reply if set, else template.
		if rule.ResponseText != nil && *rule.ResponseText != "" {
			if _, err := waClient.SendText(ctx, phone, *rule.ResponseText); err != nil {
				return false, err
			}
			category := "service"
			_ = db.InsertMessage(ctx, pool, &db.Message{
				ConversationID: convID, Direction: "outbound", MessageType: "text",
				Content: map[string]any{"body": *rule.ResponseText}, Status: "sent", Category: &category,
			})
			return true, nil
		}
		if rule.TemplateID != nil && *rule.TemplateID != "" {
			return sendTemplateAction(ctx, pool, waClient, rule, phone, convID)
		}
	}
	return false, nil
}

// sendTemplateAction sends the rule's template and records the message.
func sendTemplateAction(ctx context.Context, pool *pgxpool.Pool, waClient *whatsapp.Client,
	rule *db.AutomationRule, phone, convID string) (bool, error) {
	if rule.TemplateID == nil || *rule.TemplateID == "" {
		return false, nil
	}
	tmpl, err := db.GetTemplate(ctx, pool, *rule.TemplateID)
	if err != nil {
		return false, err
	}
	if _, err = waClient.SendTemplate(ctx, phone, tmpl.Name, tmpl.Language, nil); err != nil {
		return false, err
	}
	category := tmpl.Category
	if category == "" {
		category = "utility"
	}
	tmplID := tmpl.ID
	_ = db.InsertMessage(ctx, pool, &db.Message{
		ConversationID: convID, Direction: "outbound", MessageType: "template",
		Content: map[string]any{"template": tmpl.Name, "body": templateBody(tmpl)},
		Status:  "sent", Category: &category, TemplateID: &tmplID,
	})
	return true, nil
}

// getOrCreateTagID resolves a tag by name (case-insensitive), creating it if new.
func getOrCreateTagID(ctx context.Context, pool *pgxpool.Pool, name string) (int64, error) {
	if tags, err := db.ListTags(ctx, pool); err == nil {
		for _, t := range tags {
			if strings.EqualFold(t.Name, name) {
				return t.ID, nil
			}
		}
	}
	t, err := db.CreateTag(ctx, pool, name, "")
	if err != nil {
		return 0, err
	}
	return t.ID, nil
}

// fireWebhook POSTs the contact/event data to the rule's configured URL.
func fireWebhook(ctx context.Context, url string, rule *db.AutomationRule, contactID, phone, convID string) bool {
	payload, _ := json.Marshal(map[string]string{
		"event":           "automation_rule",
		"rule":            rule.Name,
		"contact_id":      contactID,
		"phone":           phone,
		"conversation_id": convID,
	})
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("automation webhook build (%s): %v", url, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("automation webhook POST (%s): %v", url, err)
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

// templateBody returns the BODY text of a template (for the chat preview).
func templateBody(t *db.Template) string {
	for _, c := range t.Components {
		if s, _ := c["type"].(string); strings.EqualFold(s, "BODY") {
			if txt, ok := c["text"].(string); ok {
				return txt
			}
		}
	}
	return ""
}
