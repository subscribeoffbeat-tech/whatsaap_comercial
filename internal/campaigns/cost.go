package campaigns

import "whatsapptool/internal/db"

// CalcCost returns the per-message cost for one outbound message,
// including GST: base_rate × (1 + gst_rate).
func CalcCost(category string, rates db.ConfigRates) float64 {
	var base float64
	switch category {
	case "marketing":
		base = rates.Marketing
	case "utility":
		base = rates.Utility
	case "authentication":
		base = rates.Auth
	default:
		return 0 // service messages are free
	}
	return base * (1 + rates.GSTRate)
}

// CalcTotalCost returns the total cost for count messages of a given category.
func CalcTotalCost(category string, count int, rates db.ConfigRates) float64 {
	return CalcCost(category, rates) * float64(count)
}
