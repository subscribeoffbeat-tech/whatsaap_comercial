package templates

import (
	"fmt"
	"strings"

	"whatsapptool/internal/db"
)

// editContactXData builds the Alpine x-data initializer for the edit-contact
// form, pre-filled from the contact (name, phone, email, city, consent, tags).
func editContactXData(c *db.Contact, currentTags []string) string {
	city := ""
	if v, ok := c.CustomFields["city"]; ok {
		if s, ok := v.(string); ok {
			city = s
		}
	}
	email := ""
	if c.Email != nil {
		email = *c.Email
	}
	var tagsJS strings.Builder
	tagsJS.WriteString("[")
	for i, t := range currentTags {
		if i > 0 {
			tagsJS.WriteString(",")
		}
		tagsJS.WriteString("'" + jsStr(t) + "'")
	}
	tagsJS.WriteString("]")

	optedIn := "false"
	if c.OptedIn {
		optedIn = "true"
	}
	return fmt.Sprintf("ncEditApp({name:'%s',phone:'%s',email:'%s',city:'%s',optedIn:%s,tags:%s})",
		jsStr(c.Name), jsStr(c.WAPhone), jsStr(email), jsStr(city), optedIn, tagsJS.String())
}
