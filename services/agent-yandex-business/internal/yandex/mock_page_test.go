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
	textContent   string
	textErr       error
	attributes    map[string]string
	fillCalls     []string // records Fill() calls
	fillErr       error
	clickErr      error
	clickCount    int // records number of Click() invocations
	waitErr       error
	isChecked     bool
	inputValue    string
	inputValueErr error
	setInputFiles []string // records SetInputFiles paths
	setInputErr   error
	children      map[string]*mockLocator
	allItems      []*mockLocator
	firstItem     *mockLocator
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
	m.clickCount++
	return m.clickErr
}

func (m *mockLocator) InputValue(_ ...playwright.LocatorInputValueOptions) (string, error) {
	return m.inputValue, m.inputValueErr
}

func (m *mockLocator) SetInputFiles(files interface{}, _ ...playwright.LocatorSetInputFilesOptions) error {
	switch v := files.(type) {
	case string:
		m.setInputFiles = append(m.setInputFiles, v)
	case []string:
		m.setInputFiles = append(m.setInputFiles, v...)
	}
	return m.setInputErr
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
	gotoCalls       []string
	locators        map[string]*mockLocator
	closeCalled     bool
	screenshotData  []byte
	// screenshotPaths records every Path option observed by Screenshot so
	// tests can assert which file(s) the production code asked Playwright
	// to write.
	screenshotPaths []string

	// evaluateResult is returned by Evaluate (and unmarshalable to anything
	// the production code marshals via json). Tests set this to control what
	// page.Evaluate(script) returns for free-form DOM scrapes.
	evaluateResult interface{}
	evaluateErr    error
	evaluateCalls  []string // records script bodies passed

	// keyboard, when non-nil, is returned by page.Keyboard() and lets tests
	// observe keystrokes. We embed playwright.Keyboard to satisfy the full
	// interface; only Type is overridden because that's all production uses.
	keyboard *mockKeyboard
}

func newMockPage(url string) *mockPage {
	return &mockPage{
		currentURL: url,
		locators:   make(map[string]*mockLocator),
		keyboard:   &mockKeyboard{},
	}
}

func (m *mockPage) URL() string {
	return m.currentURL
}

func (m *mockPage) Goto(url string, _ ...playwright.PageGotoOptions) (playwright.Response, error) {
	m.gotoCalls = append(m.gotoCalls, url)
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

func (m *mockPage) Screenshot(opts ...playwright.PageScreenshotOptions) ([]byte, error) {
	for _, o := range opts {
		if o.Path != nil {
			m.screenshotPaths = append(m.screenshotPaths, *o.Path)
		}
	}
	return m.screenshotData, nil
}

func (m *mockPage) Evaluate(expression string, _ ...interface{}) (interface{}, error) {
	m.evaluateCalls = append(m.evaluateCalls, expression)
	return m.evaluateResult, m.evaluateErr
}

func (m *mockPage) Keyboard() playwright.Keyboard {
	return m.keyboard
}

// keyboardStub embeds playwright.Keyboard so any unhandled method panics rather
// than silently returning zero values from the test.
type keyboardStub struct {
	playwright.Keyboard
}

// mockKeyboard records Type calls so RPA-method tests can assert payloads.
type mockKeyboard struct {
	keyboardStub
	typeCalls []string
	typeErr   error
}

func (k *mockKeyboard) Type(text string, _ ...playwright.KeyboardTypeOptions) error {
	k.typeCalls = append(k.typeCalls, text)
	return k.typeErr
}
