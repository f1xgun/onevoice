package platform

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// FactualBusinessReply recognizes only closed, exact questions and renders recorded facts.
// Other wording (including complaints, menu items and prices) has no automatic answer.
func FactualBusinessReply(business *domain.Business, question string) (string, bool) {
	normalized := strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(question), " ")), "?")
	english := false
	switch normalized {
	case "какой у вас адрес", "какой ваш адрес", "где вы находитесь", "what is your address", "where are you located":
		address := strings.TrimSpace(business.Address)
		if address == "" || strings.ContainsAny(address, "<>\r\n{}") {
			return "", false
		}
		if normalized == "what is your address" || normalized == "where are you located" {
			return "Our address: " + address, true
		}
		return "Наш адрес: " + address, true
	case "какой у вас график работы", "какие у вас часы работы", "график работы":
	case "what are your opening hours", "what are your hours":
		english = true
	default:
		return "", false
	}
	raw, err := json.Marshal(business.Settings["schedule"])
	if err != nil {
		return "", false
	}
	var days []struct {
		Day, Open, Close string
		Closed           bool
	}
	if json.Unmarshal(raw, &days) != nil || len(days) != len(dayOrder) {
		return "", false
	}
	byDay := map[string]string{}
	englishDays := map[string]string{"mon": "Mon", "tue": "Tue", "wed": "Wed", "thu": "Thu", "fri": "Fri", "sat": "Sat", "sun": "Sun"}
	for _, day := range days {
		label, known := dayRU[day.Day]
		if !known {
			return "", false
		}
		if _, duplicate := byDay[day.Day]; duplicate {
			return "", false
		}
		if english {
			label = englishDays[day.Day]
		}
		if day.Closed {
			if english {
				byDay[day.Day] = label + ": closed"
			} else {
				byDay[day.Day] = label + ": выходной"
			}
			continue
		}
		open, err := time.Parse("15:04", day.Open)
		if err != nil {
			return "", false
		}
		closeTime, err := time.Parse("15:04", day.Close)
		if err != nil || !open.Before(closeTime) {
			return "", false
		}
		byDay[day.Day] = label + " " + day.Open + "–" + day.Close
	}
	parts := make([]string, 0, len(dayOrder))
	for _, key := range dayOrder {
		parts = append(parts, byDay[key])
	}
	prefix := "Обычные часы работы: "
	if english {
		prefix = "Regular weekly hours: "
	}
	return prefix + strings.Join(parts, "; "), true
}
