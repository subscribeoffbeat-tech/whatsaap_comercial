package campaigns

import (
	"fmt"
	"strings"

	"whatsapptool/internal/db"
)

// ResolveVars resolves the ordered body parameters for a template message.
// varMap maps variable index (e.g. "1", "2") to a contact field path
// (e.g. "name", "email", "custom_fields.order_id").
// fallbacks maps the same index keys to default text used when the contact
// field is empty or absent.
// The returned slice is ordered by index 1, 2, 3, … and suitable for use
// as TemplateParameter values.
func ResolveVars(contact db.Contact, varMap, fallbacks map[string]string) []string {
	if len(varMap) == 0 {
		return nil
	}
	// Find the max index to build an ordered slice.
	maxIdx := 0
	for k := range varMap {
		var i int
		fmt.Sscanf(k, "%d", &i)
		if i > maxIdx {
			maxIdx = i
		}
	}

	params := make([]string, maxIdx)
	for i := 1; i <= maxIdx; i++ {
		key := fmt.Sprintf("%d", i)
		path := varMap[key]
		val := resolveFieldPath(contact, path)
		if val == "" {
			val = fallbacks[key]
		}
		params[i-1] = val
	}
	return params
}

func resolveFieldPath(c db.Contact, path string) string {
	switch path {
	case "name":
		return c.Name
	case "first_name":
		if parts := strings.Fields(c.Name); len(parts) > 0 {
			return parts[0]
		}
		return ""
	case "wa_phone":
		return c.WAPhone
	case "email":
		if c.Email != nil {
			return *c.Email
		}
		return ""
	case "industry":
		return c.Industry
	}
	if strings.HasPrefix(path, "custom_fields.") {
		key := strings.TrimPrefix(path, "custom_fields.")
		if v, ok := c.CustomFields[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}
