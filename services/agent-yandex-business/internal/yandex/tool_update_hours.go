package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// UpdateHours updates business operating hours in Yandex.Business via RPA.
// The Yandex.Business edit page has a single text input for hours with
// placeholder "Введите в формате «Пн-Пт 9:00-18:00»".
// hoursJSON is passed from the LLM — we convert it to the Yandex text format.
func (bb *BusinessBrowser) UpdateHours(ctx context.Context, hoursJSON string) error {
	hoursText := formatHoursForYandex(hoursJSON)
	if hoursText == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("could not parse hours from: %s", hoursJSON))
	}

	return bb.runStep(ctx, "updateHours", 3, func(page playwright.Page) error {
		if err := bb.navigateToEditPage(page); err != nil {
			return err
		}

		hoursInput := page.Locator(".WorkIntervalsUnificationInput-Input input.ya-business-input__control").First()
		if err := hoursInput.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(listItemTimeoutMs),
			State:   playwright.WaitForSelectorStateVisible,
		}); err != nil {
			debugScreenshot(page, "hours_input_not_found")
			return fmt.Errorf("hours input not found — DOM may have changed")
		}

		if err := hoursInput.Click(playwright.LocatorClickOptions{ClickCount: playwright.Int(3)}); err != nil {
			return fmt.Errorf("click hours input: %w", err)
		}
		if err := page.Keyboard().Type(hoursText, playwright.KeyboardTypeOptions{Delay: playwright.Float(keyboardDelayDefaultMs)}); err != nil {
			return fmt.Errorf("type hours: %w", err)
		}
		_ = page.Locator("h1, .InfoWorkIntervals, body").First().Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(uiPollTimeoutMs),
		})
		time.Sleep(2 * time.Second)
		debugScreenshot(page, "hours_after_fill")

		if err := clickSave(page); err != nil {
			return err
		}
		debugScreenshot(page, "hours_after_save")
		return nil
	})
}

// formatHoursForYandex converts LLM-generated hours JSON into the text format
// that Yandex.Business expects: "Пн-Пт 9:00-18:00, Сб 10:00-15:00"
func formatHoursForYandex(hoursJSON string) string {
	var structured map[string]interface{}
	if err := json.Unmarshal([]byte(hoursJSON), &structured); err != nil {
		return hoursJSON
	}

	dayMap := map[string]string{
		"monday": "Пн", "tuesday": "Вт", "wednesday": "Ср",
		"thursday": "Чт", "friday": "Пт", "saturday": "Сб", "sunday": "Вс",
		"пн": "Пн", "вт": "Вт", "ср": "Ср", "чт": "Чт",
		"пт": "Пт", "сб": "Сб", "вс": "Вс",
		"Пн": "Пн", "Вт": "Вт", "Ср": "Ср", "Чт": "Чт",
		"Пт": "Пт", "Сб": "Сб", "Вс": "Вс",
	}
	dayOrder := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

	type dayHrs struct {
		open, close string
	}
	days := make(map[string]*dayHrs)

	for key, val := range structured {
		ruDay, ok := dayMap[key]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case string:
			if v == "closed" || v == "" {
				continue
			}
			days[ruDay] = &dayHrs{open: v}
		case map[string]interface{}:
			o, _ := v["open"].(string)
			c, _ := v["close"].(string)
			if o == "" && c == "" {
				o, _ = v["start"].(string)
				c, _ = v["end"].(string)
			}
			if o != "" && c != "" {
				days[ruDay] = &dayHrs{open: o, close: c}
			}
		case []interface{}:
			if len(v) > 0 {
				if m, ok := v[0].(map[string]interface{}); ok {
					o, _ := m["open"].(string)
					c, _ := m["close"].(string)
					if o == "" && c == "" {
						o, _ = m["start"].(string)
						c, _ = m["end"].(string)
					}
					if o != "" && c != "" {
						days[ruDay] = &dayHrs{open: o, close: c}
					}
				}
			}
		}
	}

	var parts []string
	i := 0
	for i < len(dayOrder) {
		d := dayOrder[i]
		h, ok := days[d]
		if !ok {
			i++
			continue
		}
		j := i + 1
		for j < len(dayOrder) {
			nextH, ok := days[dayOrder[j]]
			if !ok || nextH.open != h.open || nextH.close != h.close {
				break
			}
			j++
		}
		var dayRange string
		if j-i == 1 {
			dayRange = d
		} else {
			dayRange = d + "-" + dayOrder[j-1]
		}
		if h.close != "" {
			parts = append(parts, fmt.Sprintf("%s %s-%s", dayRange, h.open, h.close))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", dayRange, h.open))
		}
		i = j
	}

	return strings.Join(parts, ", ")
}
