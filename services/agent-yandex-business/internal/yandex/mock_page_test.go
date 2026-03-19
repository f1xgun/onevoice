package yandex

import (
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// locatorStub is an intermediary that embeds playwright.Locator to satisfy the full
// interface. mockLocator embeds this as a named field to avoid the field/method name
// conflict with the Locator() method we override.
type locatorStub struct {
	playwright.Locator
}

// mockLocator implements playwright.Locator for testing.
// Only the methods actually called in production code are implemented.
// All other methods are provided by the embedded locatorStub (which will panic
// if called on a nil receiver — correct behavior for unexpected test calls).
type mockLocator struct {
	locatorStub
	textContent string
	textErr     error
	attributes  map[string]string
	fillCalls   []string // records Fill() calls
	fillErr     error
	clickErr    error
	waitErr     error
	isChecked   bool
	children    map[string]*mockLocator
	allItems    []*mockLocator
	firstItem   *mockLocator
}

// newMockLocator creates a mockLocator with initialized maps.
// Used by test files that need to set up Playwright DOM mocks.
var _ = newMockLocator // ensure it's not flagged as unused

func newMockLocator() *mockLocator {
	return &mockLocator{
		attributes: make(map[string]string),
		children:   make(map[string]*mockLocator),
	}
}

func (m *mockLocator) TextContent(_ ...playwright.LocatorTextContentOptions) (string, error) {
	return m.textContent, m.textErr
}

func (m *mockLocator) GetAttribute(name string, _ ...playwright.LocatorGetAttributeOptions) (string, error) {
	if v, ok := m.attributes[name]; ok {
		return v, nil
	}
	return "", nil
}

func (m *mockLocator) Fill(value string, _ ...playwright.LocatorFillOptions) error {
	m.fillCalls = append(m.fillCalls, value)
	return m.fillErr
}

func (m *mockLocator) Click(_ ...playwright.LocatorClickOptions) error {
	return m.clickErr
}

func (m *mockLocator) WaitFor(_ ...playwright.LocatorWaitForOptions) error {
	return m.waitErr
}

func (m *mockLocator) IsChecked(_ ...playwright.LocatorIsCheckedOptions) (bool, error) {
	return m.isChecked, nil
}

func (m *mockLocator) Locator(selectorOrLocator interface{}, _ ...playwright.LocatorLocatorOptions) playwright.Locator {
	selector, _ := selectorOrLocator.(string)
	if child, ok := m.children[selector]; ok {
		return child
	}
	return &mockLocator{waitErr: fmt.Errorf("selector not found: %s", selector)}
}

func (m *mockLocator) First() playwright.Locator {
	if m.firstItem != nil {
		return m.firstItem
	}
	return m
}

func (m *mockLocator) All() ([]playwright.Locator, error) {
	result := make([]playwright.Locator, len(m.allItems))
	for i, item := range m.allItems {
		result[i] = item
	}
	return result, nil
}

// mockBrowserContext implements playwright.BrowserContext for testing.
type mockBrowserContext struct {
	playwright.BrowserContext
	closeCalled bool
}

func (m *mockBrowserContext) Close(_ ...playwright.BrowserContextCloseOptions) error {
	m.closeCalled = true
	return nil
}

// mockPage implements playwright.Page for testing.
type mockPage struct {
	playwright.Page // embed for unused methods
	currentURL      string
	gotoErr         error
	locators        map[string]*mockLocator
	closeCalled     bool
	screenshotData  []byte
}

func newMockPage(url string) *mockPage {
	return &mockPage{
		currentURL: url,
		locators:   make(map[string]*mockLocator),
	}
}

func (m *mockPage) URL() string {
	return m.currentURL
}

func (m *mockPage) Goto(url string, _ ...playwright.PageGotoOptions) (playwright.Response, error) {
	if m.gotoErr != nil {
		return nil, m.gotoErr
	}
	if m.currentURL == "" {
		m.currentURL = url
	}
	return nil, nil
}

func (m *mockPage) Locator(selector string, _ ...playwright.PageLocatorOptions) playwright.Locator {
	if loc, ok := m.locators[selector]; ok {
		return loc
	}
	return &mockLocator{waitErr: fmt.Errorf("selector not found: %s", selector)}
}

func (m *mockPage) Close(_ ...playwright.PageCloseOptions) error {
	m.closeCalled = true
	return nil
}

func (m *mockPage) Screenshot(_ ...playwright.PageScreenshotOptions) ([]byte, error) {
	return m.screenshotData, nil
}
