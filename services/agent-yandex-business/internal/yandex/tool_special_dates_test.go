package yandex

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func specialRow(date, open, closeTime string, closed bool) *mockLocator {
	row := newMockLocator()
	row.children[specialDateInput] = &mockLocator{inputValue: date}
	row.children[specialOpenInput] = &mockLocator{inputValue: open}
	row.children[specialCloseInput] = &mockLocator{inputValue: closeTime}
	row.children[specialClosedInput] = &mockLocator{isChecked: closed}
	row.children[specialDeleteButton] = newMockLocator()
	return row
}

func TestSpecialDatesEditClosureRemovalPreservesUnrelatedAndRetries(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	panel := newMockLocator()
	rows := newMockLocator()
	panel.children[specialRows] = rows
	page.locators[specialPanel] = panel
	edited := specialRow("04.11.2026", "09:00", "18:00", false)
	removed := specialRow("05.11.2026", "10:00", "17:00", false)
	unrelated := specialRow("06.11.2026", "11:00", "16:00", false)
	rows.allItems = []*mockLocator{edited, removed, unrelated}
	removed.children[specialDeleteButton].clickFn = func() { rows.allItems = []*mockLocator{edited, unrelated} }
	change := domain.SpecialDateHoursChange{Previous: []domain.SpecialDateHours{{Date: "2026-11-04", Open: "09:00", Close: "18:00"}, {Date: "2026-11-05", Open: "10:00", Close: "17:00"}}, Current: []domain.SpecialDateHours{{Date: "2026-11-04", Closed: true}}}
	require.NoError(t, applySpecialDates(context.Background(), page, change))
	require.True(t, edited.children[specialClosedInput].isChecked)
	require.Equal(t, 1, removed.children[specialDeleteButton].clickCount)
	require.Empty(t, unrelated.children[specialDateInput].fillCalls)
	require.Equal(t, 0, unrelated.children[specialDeleteButton].clickCount)
	require.NoError(t, applySpecialDates(context.Background(), page, change))
	require.Equal(t, 1, removed.children[specialDeleteButton].clickCount)
}

func TestSpecialDatesAdditionEditingAndLostReceiptRetry(t *testing.T) {
	for _, existingBlank := range []bool{true, false} {
		t.Run(map[bool]string{true: "reuse default blank", false: "append alongside unrelated date"}[existingBlank], func(t *testing.T) {
			page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
			panel, rows := newMockLocator(), newMockLocator()
			panel.children[specialRows] = rows
			page.locators[specialPanel] = panel
			unrelated := specialRow("06.11.2026", "11:00", "16:00", false)
			blank := specialRow("02.11.2026", "", "", false)
			rows.allItems = []*mockLocator{unrelated}
			if existingBlank {
				rows.allItems = append(rows.allItems, blank)
			}
			add := newMockLocator()
			panel.children[specialAddButton] = add
			add.clickFn = func() { rows.allItems = append(rows.allItems, blank) }
			change := domain.SpecialDateHoursChange{Previous: []domain.SpecialDateHours{}, Current: []domain.SpecialDateHours{{Date: "2026-11-04", Open: "09:00", Close: "18:00"}}}
			require.NoError(t, applySpecialDates(context.Background(), page, change))
			require.Equal(t, "04.11.2026", blank.children[specialDateInput].inputValue)
			require.Equal(t, "09:00", blank.children[specialOpenInput].inputValue)
			require.NoError(t, applySpecialDates(context.Background(), page, change))
			require.Len(t, blank.children[specialDateInput].fillCalls, 1, "retry must rescan rather than create duplicate")
			if existingBlank {
				require.Equal(t, 0, add.clickCount)
			} else {
				require.Equal(t, 1, add.clickCount)
			}
			change.Previous = change.Current
			change.Current = []domain.SpecialDateHours{{Date: "2026-11-04", Open: "10:00", Close: "17:00"}}
			require.NoError(t, applySpecialDates(context.Background(), page, change))
			require.Equal(t, "10:00", blank.children[specialOpenInput].inputValue)
			require.Equal(t, "17:00", blank.children[specialCloseInput].inputValue)
			require.Equal(t, "11:00", unrelated.children[specialOpenInput].inputValue)
		})
	}
}

func TestSpecialDatesSaveReloadsAndLostReceiptDoesNotDuplicate(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	panel, rows := newMockLocator(), newMockLocator()
	panel.children[specialRows] = rows
	panel.children[specialDateInput] = newMockLocator()
	overlay := newMockLocator()
	panel.children[".InfoModal-Overlay"] = overlay
	page.locators[specialPanel] = panel
	open := newMockLocator()
	page.locators["role=button[name=Добавить режим работы]"] = open
	save := newMockLocator()
	page.locators[".SaveButton-Button"] = save
	blank := specialRow("02.11.2026", "", "", false)
	rows.allItems = []*mockLocator{blank}
	attempts := 0
	save.clickFn = func() {
		attempts++
		if attempts == 1 {
			save.clickErr = errors.New("lost save response")
		} else {
			save.clickErr = nil
		}
	}
	bb := businessBrowserOnPage(page, "123")
	change := domain.SpecialDateHoursChange{Previous: []domain.SpecialDateHours{}, Current: []domain.SpecialDateHours{{Date: "2026-11-04", Closed: true}}}
	require.NoError(t, bb.UpdateSpecialDates(context.Background(), change))
	require.Equal(t, 2, save.clickCount)
	require.Len(t, blank.children[specialDateInput].fillCalls, 1)
	require.Len(t, page.gotoCalls, 3, "retry must navigate again, and success must reload for readback")
	require.Equal(t, 2, overlay.clickCount)
}

func TestSpecialDatesRebindsAfterDateRerendersAndReordersRows(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	panel, rows := newMockLocator(), newMockLocator()
	panel.children[specialRows] = rows
	page.locators[specialPanel] = panel
	blank := specialRow("02.11.2026", "", "", false)
	unrelated := specialRow("06.11.2026", "11:00", "16:00", false)
	desired := newMockLocator()
	desired.children = blank.children
	rows.allItems = []*mockLocator{blank, unrelated}
	blank.children[specialDateInput].fillFn = func() {
		// The old nth=0 locator now resolves to the unrelated row after rerender.
		blank.children = unrelated.children
		rows.allItems = []*mockLocator{unrelated, desired}
	}
	change := domain.SpecialDateHoursChange{Current: []domain.SpecialDateHours{{Date: "2026-11-04", Open: "09:00", Close: "18:00"}}}
	require.NoError(t, applySpecialDates(context.Background(), page, change))
	require.Equal(t, "09:00", desired.children[specialOpenInput].inputValue)
	require.Equal(t, "18:00", desired.children[specialCloseInput].inputValue)
	require.Equal(t, "11:00", unrelated.children[specialOpenInput].inputValue)
	require.Equal(t, "16:00", unrelated.children[specialCloseInput].inputValue)
	require.False(t, unrelated.children[specialClosedInput].isChecked)
}

func TestSpecialDatesDateRebindFailsClosed(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "date disappeared", true: "date became ambiguous"}[ambiguous], func(t *testing.T) {
			page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
			panel, rows := newMockLocator(), newMockLocator()
			panel.children[specialRows] = rows
			page.locators[specialPanel] = panel
			blank := specialRow("02.11.2026", "", "", false)
			unrelated := specialRow("06.11.2026", "11:00", "16:00", false)
			rows.allItems = []*mockLocator{blank, unrelated}
			blank.children[specialDateInput].fillFn = func() {
				rows.allItems = []*mockLocator{unrelated}
				if ambiguous {
					rows.allItems = append(rows.allItems, blank, specialRow("04.11.2026", "", "", false))
				}
			}
			change := domain.SpecialDateHoursChange{Current: []domain.SpecialDateHours{{Date: "2026-11-04", Closed: true}}}
			require.Error(t, applySpecialDates(context.Background(), page, change))
			require.False(t, unrelated.children[specialClosedInput].isChecked)
			require.False(t, blank.children[specialClosedInput].isChecked)
		})
	}
}
