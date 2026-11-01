package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// Selectors observed read-only in the provider editor on 2026-11-01. Keep
// every field scoped to the holiday panel; weekly/profile fields are untouched.
const (
	specialPanel        = ".InfoModal:has(.WorkIntervalsHoliday)"
	specialRows         = ".WorkIntervalsHoliday"
	specialDateInput    = ".WorkIntervalsHoliday-Calendar input"
	specialOpenInput    = ".WorkIntervalsHoliday-InputTime input >> nth=0"
	specialCloseInput   = ".WorkIntervalsHoliday-InputTime input >> nth=1"
	specialClosedInput  = ".WorkIntervalsHoliday-Checkbox input"
	specialDeleteButton = ".WorkIntervalsHoliday-Bin"
	specialAddButton    = ".InfoWorkIntervalsHolidays-Add button"
)

type specialDateRow struct {
	locator playwright.Locator
	hours   domain.SpecialDateHours
	blank   bool
}

// UpdateSpecialDates shares the existing session, tenant, retry and page locks.
// Every attempt navigates and rescans: a save followed by a lost receipt must
// find the existing date rather than append another row.
func (bb *BusinessBrowser) UpdateSpecialDates(ctx context.Context, change domain.SpecialDateHoursChange) error {
	raw, err := json.Marshal(change)
	if err != nil {
		return a2a.NewNonRetryableError(err)
	}
	if _, err := domain.ParseSpecialDateHoursChange(raw); err != nil {
		return a2a.NewNonRetryableError(err)
	}
	return bb.runStep(ctx, "updateSpecialDates", 3, func(page playwright.Page) error {
		if err := bb.openSpecialDates(page); err != nil {
			return err
		}
		if err := applySpecialDates(ctx, page, change); err != nil {
			debugScreenshot(page, "special_dates_edit_failed")
			return err
		}
		if err := page.Locator(specialPanel).Locator(".InfoModal-Overlay").Click(); err != nil {
			return fmt.Errorf("close holiday panel: %w", err)
		}
		if err := clickSave(page); err != nil {
			return err
		}
		// Re-navigation confirms persistence, rather than merely reading local form state.
		if err := bb.openSpecialDates(page); err != nil {
			return err
		}
		rows, err := readSpecialDates(page)
		if err != nil {
			return err
		}
		for _, want := range change.Current {
			row, ok := findSpecialDate(rows, want.Date)
			if !ok || !sameSpecialHours(row.hours, want) {
				return fmt.Errorf("special date %s not confirmed after save", want.Date)
			}
		}
		for _, old := range change.Previous {
			if containsSpecialDate(change.Current, old.Date) {
				continue
			}
			if _, ok := findSpecialDate(rows, old.Date); ok {
				return fmt.Errorf("special date %s removal not confirmed", old.Date)
			}
		}
		return nil
	})
}

func (bb *BusinessBrowser) openSpecialDates(page playwright.Page) error {
	if err := bb.navigateToEditPage(page); err != nil {
		return err
	}
	if err := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Добавить режим работы", Exact: playwright.Bool(true)}).Click(); err != nil {
		return fmt.Errorf("open holiday panel: %w", err)
	}
	return page.Locator(specialPanel).Locator(specialDateInput).First().WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(listItemTimeoutMs)})
}

func readSpecialDates(page playwright.Page) ([]specialDateRow, error) {
	locators, err := page.Locator(specialPanel).Locator(specialRows).All()
	if err != nil {
		return nil, fmt.Errorf("read holiday rows: %w", err)
	}
	rows := make([]specialDateRow, 0, len(locators))
	seen := map[string]bool{}
	for _, loc := range locators {
		date, err := loc.Locator(specialDateInput).InputValue()
		if err != nil {
			return nil, err
		}
		parsed, err := time.Parse("02.01.2006", date)
		if err != nil {
			return nil, fmt.Errorf("unrecognized provider date %q", date)
		}
		open, err := loc.Locator(specialOpenInput).InputValue()
		if err != nil {
			return nil, err
		}
		closeTime, err := loc.Locator(specialCloseInput).InputValue()
		if err != nil {
			return nil, err
		}
		closed, err := loc.Locator(specialClosedInput).IsChecked()
		if err != nil {
			return nil, err
		}
		row := specialDateRow{locator: loc, hours: domain.SpecialDateHours{Date: parsed.Format("2006-01-02"), Open: open, Close: closeTime, Closed: closed}, blank: !closed && open == "" && closeTime == ""}
		if !row.blank {
			if seen[row.hours.Date] {
				return nil, a2a.NewNonRetryableError(fmt.Errorf("duplicate provider special date %s; manual review required", row.hours.Date))
			}
			seen[row.hours.Date] = true
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func applySpecialDates(ctx context.Context, page playwright.Page, change domain.SpecialDateHoursChange) error {
	for _, want := range change.Current {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := readSpecialDates(page)
		if err != nil {
			return err
		}
		row, found := findSpecialDate(rows, want.Date)
		if found && sameSpecialHours(row.hours, want) {
			continue
		}
		if !found {
			var blank bool
			for _, candidate := range rows {
				if candidate.blank {
					row = candidate
					blank = true
					break
				}
			}
			if !blank {
				if err := page.Locator(specialPanel).Locator(specialAddButton).Click(); err != nil {
					return fmt.Errorf("add holiday row: %w", err)
				}
				rows, err = readSpecialDates(page)
				if err != nil {
					return err
				}
				for _, candidate := range rows {
					if candidate.blank {
						row = candidate
						blank = true
						break
					}
				}
			}
			if !blank {
				return fmt.Errorf("no blank holiday row after add")
			}
			parsed, err := time.Parse("2006-01-02", want.Date)
			if err != nil {
				return err
			}
			input := row.locator.Locator(specialDateInput)
			if err := input.Fill(parsed.Format("02.01.2006")); err != nil {
				return err
			}
			if err := input.Blur(); err != nil {
				return fmt.Errorf("commit holiday date: %w", err)
			}
		}
		// Date edits may reorder rows. Never retain an nth-based row locator
		// across a commit; uniquely rebind by the requested calendar date.
		row, err = bindSpecialDate(page, want.Date)
		if err != nil {
			return err
		}
		if err := row.locator.Locator(specialClosedInput).SetChecked(want.Closed); err != nil {
			return err
		}
		if !want.Closed {
			row, err = bindSpecialDate(page, want.Date)
			if err != nil {
				return err
			}
			if err := row.locator.Locator(specialOpenInput).Fill(want.Open); err != nil {
				return err
			}
			row, err = bindSpecialDate(page, want.Date)
			if err != nil {
				return err
			}
			if err := row.locator.Locator(specialCloseInput).Fill(want.Close); err != nil {
				return err
			}
			row, err = bindSpecialDate(page, want.Date)
			if err != nil {
				return err
			}
			breaks, err := row.locator.Locator(".WorkIntervalsHoliday-InputBreaks input").All()
			if err != nil {
				return err
			}
			for _, input := range breaks {
				if err := input.Fill(""); err != nil {
					return err
				}
			}
		}
	}
	for _, old := range change.Previous {
		if err := ctx.Err(); err != nil {
			return err
		}
		if containsSpecialDate(change.Current, old.Date) {
			continue
		}
		rows, err := readSpecialDates(page)
		if err != nil {
			return err
		}
		if row, found := findSpecialDate(rows, old.Date); found {
			if err := row.locator.Locator(specialDeleteButton).Click(); err != nil {
				return fmt.Errorf("remove owned holiday: %w", err)
			}
		}
	}
	return nil
}

func findSpecialDate(rows []specialDateRow, date string) (specialDateRow, bool) {
	for _, row := range rows {
		if !row.blank && row.hours.Date == date {
			return row, true
		}
	}
	return specialDateRow{}, false
}
func containsSpecialDate(dates []domain.SpecialDateHours, date string) bool {
	for _, day := range dates {
		if day.Date == date {
			return true
		}
	}
	return false
}
func sameSpecialHours(a, b domain.SpecialDateHours) bool {
	return a.Date == b.Date && a.Closed == b.Closed && (a.Closed || (a.Open == b.Open && a.Close == b.Close))
}

// bindSpecialDate includes the just-committed, still-empty row. Missing or
// ambiguous dates fail closed before any hours or closure control is touched.
func bindSpecialDate(page playwright.Page, date string) (specialDateRow, error) {
	rows, err := readSpecialDates(page)
	if err != nil {
		return specialDateRow{}, err
	}
	var match specialDateRow
	count := 0
	for _, row := range rows {
		if row.hours.Date == date {
			match = row
			count++
		}
	}
	if count != 1 {
		return specialDateRow{}, a2a.NewNonRetryableError(fmt.Errorf("cannot uniquely bind holiday date %s (%d rows)", date, count))
	}
	return match, nil
}
