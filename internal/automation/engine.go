package automation

import (
	"context"
	"log"
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

	for _, rule := range rules {
		if !matchesRule(rule, text, isNewContact, now) {
			continue
		}

		fired, err := executeRule(ctx, pool, waClient, rule, phone, convID)
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
func matchesRule(rule *db.AutomationRule, text string, isNewContact bool, now time.Time) bool {
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
		return campaigns.IsQuietHours(now)
	}
	return false
}

// executeRule sends the rule's response and returns true if it sent something.
func executeRule(
	ctx context.Context,
	pool *pgxpool.Pool,
	waClient *whatsapp.Client,
	rule *db.AutomationRule,
	phone, convID string,
) (bool, error) {
	switch {
	case rule.ResponseText != nil && *rule.ResponseText != "":
		_, err := waClient.SendText(ctx, phone, *rule.ResponseText)
		if err != nil {
			return false, err
		}
		// Persist outbound message for the conversation.
		category := "service"
		dbMsg := &db.Message{
			ConversationID: convID,
			Direction:      "outbound",
			MessageType:    "text",
			Content:        map[string]any{"body": *rule.ResponseText},
			Status:         "sent",
			Category:       &category,
		}
		if err := db.InsertMessage(ctx, pool, dbMsg); err != nil {
			log.Printf("automation rule %d: persist message: %v", rule.ID, err)
		}
		return true, nil

	case rule.TemplateID != nil && *rule.TemplateID != "":
		tmpl, err := db.GetTemplate(ctx, pool, *rule.TemplateID)
		if err != nil {
			return false, err
		}
		_, err = waClient.SendTemplate(ctx, phone, tmpl.Name, tmpl.Language, nil)
		if err != nil {
			return false, err
		}
		category := "utility"
		tmplID := tmpl.ID
		dbMsg := &db.Message{
			ConversationID: convID,
			Direction:      "outbound",
			MessageType:    "template",
			Content:        map[string]any{"template": tmpl.Name},
			Status:         "sent",
			Category:       &category,
			TemplateID:     &tmplID,
		}
		if err := db.InsertMessage(ctx, pool, dbMsg); err != nil {
			log.Printf("automation rule %d: persist template message: %v", rule.ID, err)
		}
		return true, nil
	}
	return false, nil
}
