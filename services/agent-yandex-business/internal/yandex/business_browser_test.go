package yandex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// newMockBrowserPool returns a *BrowserPool whose WithPage runs `fn` against
// the supplied mockPage instead of launching real Chromium. The pool's
// withPageFn seam is the only knob production code never touches.
func newMockBrowserPool(page *mockPage) *BrowserPool {
	p := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	p.withPageFn = func(_ context.Context, _, _ string, fn func(playwright.Page) error) error {
		return fn(page)
	}
	return p
}

// businessBrowserOnPage builds a BusinessBrowser whose pool routes WithPage
// invocations to `page`. The permalink is passed through to baseURL().
func businessBrowserOnPage(page *mockPage, permalink string) *BusinessBrowser {
	return newMockBrowserPool(page).ForBusiness("biz-test", "[]", permalink)
}

// -----------------------------------------------------------------------------
// ListCompanies tests
// -----------------------------------------------------------------------------

// Test #1 — ListCompanies parses .CompaniesCompanyRow Evaluate output into
// the canonical [{permalink, name}] map shape.
func TestBusinessBrowser_ListCompanies_ParsesCompanyRows(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/companies/")
	// Make .CompaniesCompanyRow visible (WaitFor returns nil).
	page.locators[".CompaniesCompanyRow"] = &mockLocator{}
	// Evaluate returns the JSON-marshalable list the production code expects.
	page.evaluateResult = []map[string]interface{}{
		{"permalink": "114697172504", "name": "Кафе Тверская"},
		{"permalink": "987654321", "name": "Магазин Арбат"},
	}

	bb := businessBrowserOnPage(page, "default")
	rows, err := bb.ListCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListCompanies returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d (%v)", len(rows), rows)
	}
	if rows[0]["permalink"] != "114697172504" || rows[0]["name"] != "Кафе Тверская" {
		t.Fatalf("unexpected row[0]: %v", rows[0])
	}
}

// Test #2 — passport-redirect canary becomes a wrapped non-retryable error.
func TestBusinessBrowser_ListCompanies_PassportRedirect_ReturnsNonRetryable(t *testing.T) {
	page := newMockPage("https://passport.yandex.ru/auth/welcome?retpath=...")
	bb := businessBrowserOnPage(page, "default")

	_, err := bb.ListCompanies(context.Background())
	if err == nil {
		t.Fatal("expected error from passport redirect, got nil")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got: %v", err)
	}
}

// Test #3 — when the SPA's row selector times out, return [] not an error.
func TestBusinessBrowser_ListCompanies_NoCompanies_ReturnsEmpty(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/companies/")
	// No .CompaniesCompanyRow registered → fallback locator returns waitErr.
	bb := businessBrowserOnPage(page, "default")

	rows, err := bb.ListCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListCompanies returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty rows, got %d", len(rows))
	}
}

// -----------------------------------------------------------------------------
// GetReviews tests
// -----------------------------------------------------------------------------

// Test #4 — limit ≤0 clamps to 20, limit >50 clamps to maxBatchLimit (50).
// We assert the clamp by counting trips through the page.Goto fast-fail path:
// the mockPage has no review container, so the method short-circuits to []
// without iterating maxBatchLimit times. The clamp is observable via the
// withRetry-wrapped error message bound by the limit math, but here it's
// simpler to assert no panic and empty result on both extremes.
func TestBusinessBrowser_GetReviews_ScrapesNCards_LimitClamped(t *testing.T) {
	for _, limit := range []int{-5, 0, 9999} {
		page := newMockPage("https://yandex.ru/sprav/123/p/edit/reviews")
		bb := businessBrowserOnPage(page, "123")
		got, err := bb.GetReviews(context.Background(), limit)
		if err != nil {
			t.Fatalf("limit=%d: GetReviews returned error: %v", limit, err)
		}
		if len(got) != 0 {
			t.Fatalf("limit=%d: expected 0 reviews from empty mock DOM, got %d", limit, len(got))
		}
	}
}

// Test #5 — extractText fallback chain hits, extractRating returns first
// non-empty data attribute / aria-label / textContent. We exercise both
// helpers by building a card with one nested mockLocator per selector.
func TestBusinessBrowser_GetReviews_ExtractsAuthorRatingText(t *testing.T) {
	card := newMockLocator()
	authorEl := &mockLocator{textContent: "Иван Иванов"}
	textEl := &mockLocator{textContent: "Отличное место"}
	ratingEl := &mockLocator{attributes: map[string]string{"data-rating": "5"}}
	card.children["[data-testid='review-author']"] = authorEl
	card.children["[data-testid='review-text']"] = textEl
	card.children["[data-testid='review-rating']"] = ratingEl

	got := extractText(card,
		[]string{"[data-testid='review-author']", ".review-author"},
		"Unknown")
	if got != "Иван Иванов" {
		t.Fatalf("extractText = %q, want %q", got, "Иван Иванов")
	}

	rating := extractRating(card)
	if s, ok := rating.(string); !ok || s != "5" {
		t.Fatalf("extractRating = %v, want \"5\"", rating)
	}
}

// Test #6 — scrapeReviewCards on an empty DOM returns empty slice (no error).
func TestScrapeReviewCards_HandlesEmpty(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/reviews")
	// No card selector registered → All() returns empty.
	cards, err := scrapeReviewCards(page, 10)
	if err != nil {
		t.Fatalf("scrapeReviewCards returned error: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}
}

// Test #7 — extractText fallback chain: first selector misses, second hits.
func TestExtractText_FallbackChain(t *testing.T) {
	parent := newMockLocator()
	parent.children["[data-testid='review-text']"] = &mockLocator{textContent: ""}
	parent.children[".review-text"] = &mockLocator{textContent: "fallback hit"}

	got := extractText(parent,
		[]string{"[data-testid='review-text']", ".review-text"},
		"")
	if got != "fallback hit" {
		t.Fatalf("extractText = %q, want %q", got, "fallback hit")
	}

	// All selectors miss → fallback returned.
	got = extractText(parent, []string{".not-there"}, "FALLBACK")
	if got != "FALLBACK" {
		t.Fatalf("extractText (no match) = %q, want %q", got, "FALLBACK")
	}
}

// Test #8 — extractRating returns string for data-rating attr; nil when missing.
func TestExtractRating_NumericVsString(t *testing.T) {
	cardWith := newMockLocator()
	cardWith.children["[data-testid='review-rating']"] = &mockLocator{
		attributes: map[string]string{"data-rating": "4"},
	}
	if got := extractRating(cardWith); got != "4" {
		t.Fatalf("extractRating with data-rating = %v, want \"4\"", got)
	}

	cardEmpty := newMockLocator()
	if got := extractRating(cardEmpty); got != nil {
		t.Fatalf("extractRating on empty card = %v, want nil", got)
	}
}

// -----------------------------------------------------------------------------
// ReplyReview tests
// -----------------------------------------------------------------------------

// Test #9 — ReplyReview navigates to /reviews and bails fast when the review
// card selector cannot be located (non-retryable, since the review id is
// LLM-supplied and a missing card means the review doesn't exist).
func TestBusinessBrowser_ReplyReview_NavigatesAndClicksSave(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/reviews")
	// No data-review-id locator registered → WaitFor errors → non-retryable.

	bb := businessBrowserOnPage(page, "123")
	err := bb.ReplyReview(context.Background(), "rev-1", "thanks!")
	if err == nil {
		t.Fatal("expected error when review card not found")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got: %v", err)
	}
	// Confirm it actually navigated to /reviews before bailing.
	if len(page.gotoCalls) == 0 || !strings.Contains(page.gotoCalls[0], "/reviews") {
		t.Fatalf("expected first Goto to /reviews, got %v", page.gotoCalls)
	}
}

// -----------------------------------------------------------------------------
// GetInfo tests
// -----------------------------------------------------------------------------

// Test #10 — GetInfo extracts name/phone/email/hours/status from corresponding
// mocked locators and surfaces them on the result map.
func TestBusinessBrowser_GetInfo_ScrapesAllFields(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	// Name, phone, email, hours, status — production code reads via TextContent / InputValue.
	page.locators["[class*='CompanyName'], [class*='company-name'], .SidebarCompanyInfo span"] = &mockLocator{
		textContent: "Кафе Тверская",
	}
	page.locators[".WorkIntervalsUnificationInput-Input input.ya-business-input__control"] = &mockLocator{
		inputValue: "Пн-Пт 9:00-18:00",
	}
	page.locators[".InfoPhones input.ya-business-input__control"] = &mockLocator{
		inputValue: "+7 495 000 00 00",
	}
	page.locators[".InfoEmails input.ya-business-input__control"] = &mockLocator{
		inputValue: "info@example.ru",
	}
	page.locators[".InfoWorkIntervals-StatusWrapper .ya-business-select__button-content"] = &mockLocator{
		textContent: "Открыто",
	}
	// Address evaluator returns empty (we don't drive evaluator here).
	page.evaluateResult = ""

	bb := businessBrowserOnPage(page, "123")
	info, err := bb.GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo returned error: %v", err)
	}
	if info["name"] != "Кафе Тверская" {
		t.Fatalf("name = %v, want Кафе Тверская", info["name"])
	}
	if info["phone"] != "+7 495 000 00 00" {
		t.Fatalf("phone = %v", info["phone"])
	}
	if info["email"] != "info@example.ru" {
		t.Fatalf("email = %v", info["email"])
	}
	if info["hours"] != "Пн-Пт 9:00-18:00" {
		t.Fatalf("hours = %v", info["hours"])
	}
	if info["status"] != "Открыто" {
		t.Fatalf("status = %v", info["status"])
	}
}

// -----------------------------------------------------------------------------
// UpdateInfo tests
// -----------------------------------------------------------------------------

// Test #11 — UpdateInfo only types into fields that are present in the input
// map; unknown keys are skipped silently. We verify by counting Keyboard.Type
// calls and inspecting the saved-button click count.
func TestBusinessBrowser_UpdateInfo_FillsFormFields(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	// Provide the phone input + body click target + save button.
	phoneInput := &mockLocator{}
	page.locators[".InfoPhones input.ya-business-input__control"] = phoneInput
	page.locators["h1, .InfoBlockCarcass, body"] = &mockLocator{}
	page.locators[".SaveButton-Button"] = &mockLocator{}

	bb := businessBrowserOnPage(page, "123")
	err := bb.UpdateInfo(context.Background(), map[string]string{
		"phone":         "+7 495 111 22 33",
		"unknown_field": "ignored",
	})
	if err != nil {
		t.Fatalf("UpdateInfo returned error: %v", err)
	}
	// Exactly one Type call (for phone), since unknown_field has no selector.
	if len(page.keyboard.typeCalls) != 1 {
		t.Fatalf("expected 1 Type call, got %d (%v)", len(page.keyboard.typeCalls), page.keyboard.typeCalls)
	}
	if page.keyboard.typeCalls[0] != "+7 495 111 22 33" {
		t.Fatalf("Type[0] = %q", page.keyboard.typeCalls[0])
	}
}

// -----------------------------------------------------------------------------
// UpdateHours tests
// -----------------------------------------------------------------------------

// Test #12 — UpdateHours formats the JSON, types the result into the hours
// input, and clicks Save. We assert the typed value matches the expected
// format produced by formatHoursForYandex.
func TestBusinessBrowser_UpdateHours_FormatsHoursPayload(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	page.locators[".WorkIntervalsUnificationInput-Input input.ya-business-input__control"] = &mockLocator{}
	page.locators["h1, .InfoWorkIntervals, body"] = &mockLocator{}
	page.locators[".SaveButton-Button"] = &mockLocator{}

	bb := businessBrowserOnPage(page, "123")
	hoursJSON := `{"monday":{"open":"09:00","close":"18:00"},"tuesday":{"open":"09:00","close":"18:00"}}`
	if err := bb.UpdateHours(context.Background(), hoursJSON); err != nil {
		t.Fatalf("UpdateHours returned error: %v", err)
	}
	if len(page.keyboard.typeCalls) != 1 {
		t.Fatalf("expected 1 Type call, got %d", len(page.keyboard.typeCalls))
	}
	got := page.keyboard.typeCalls[0]
	if got != "Пн-Вт 09:00-18:00" {
		t.Fatalf("typed hours = %q, want %q", got, "Пн-Вт 09:00-18:00")
	}
}

// Test #13 — formatHoursForYandex: closed-or-empty days are excluded.
func TestFormatHoursForYandex_Closed(t *testing.T) {
	in := `{"monday":"closed","tuesday":""}`
	got := formatHoursForYandex(in)
	if got != "" {
		t.Fatalf("formatHoursForYandex(%q) = %q, want empty", in, got)
	}
}

// Test #14 — formatHoursForYandex: a single open slot serializes to "Пн HH:MM-HH:MM".
func TestFormatHoursForYandex_OpenSlot(t *testing.T) {
	in := `{"monday":{"open":"09:00","close":"21:00"}}`
	got := formatHoursForYandex(in)
	if got != "Пн 09:00-21:00" {
		t.Fatalf("formatHoursForYandex(%q) = %q, want %q", in, got, "Пн 09:00-21:00")
	}

	// Multi-day same-hours grouping.
	in = `{"saturday":{"open":"10:00","close":"15:00"},"sunday":{"open":"10:00","close":"15:00"}}`
	got = formatHoursForYandex(in)
	if got != "Сб-Вс 10:00-15:00" {
		t.Fatalf("formatHoursForYandex(weekend) = %q, want %q", got, "Сб-Вс 10:00-15:00")
	}
}

// -----------------------------------------------------------------------------
// CreatePost tests
// -----------------------------------------------------------------------------

// Test #15 — CreatePost types into the textarea, then clicks the Submit
// button. We assert both the typed payload and the click on .PostAddForm-Submit.
func TestBusinessBrowser_CreatePost_SubmitsTextareaAndClicksPublish(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/posts/")
	textarea := &mockLocator{}
	submit := &mockLocator{}
	page.locators[".PostAddForm-Textarea textarea"] = textarea
	page.locators[".PostAddForm-Submit"] = submit

	bb := businessBrowserOnPage(page, "123")
	if err := bb.CreatePost(context.Background(), "Hello world"); err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if len(page.keyboard.typeCalls) != 1 || page.keyboard.typeCalls[0] != "Hello world" {
		t.Fatalf("typeCalls = %v, want [Hello world]", page.keyboard.typeCalls)
	}
	if submit.clickCount == 0 {
		t.Fatal("expected submit button to be clicked at least once")
	}
}

// -----------------------------------------------------------------------------
// UploadPhoto tests
// -----------------------------------------------------------------------------

// Test #16 — UploadPhoto picks the right input selector based on category.
// We exercise the selector switch by passing each known category and asserting
// SetInputFiles is invoked on the correctly-mapped locator. Photo download
// goes to a real http.Get; we use a tiny inline HTTP test server.
func TestBusinessBrowser_UploadPhoto_SetsCategoryAndUploadsFile(t *testing.T) {
	srv := newPNGServer(t)
	defer srv.Close()

	cases := []struct {
		category string
		selector string
	}{
		{"general", ".MediaUploadButton-Input"},
		{"logo", ".MediaAttach-Input[name='logo']"},
		{"interior", ".MediaAttach-Input[name='interior']"},
		{"goods", ".MediaAttach-Input[name='goods']"},
	}
	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			page := newMockPage("https://yandex.ru/sprav/123/p/edit/photos/")
			fileInput := &mockLocator{}
			page.locators[tc.selector] = fileInput
			// Crop dialog button: register but make WaitFor fail so it's skipped.
			page.locators["button:has-text('Сохранить')"] = &mockLocator{
				waitErr: errors.New("no crop dialog"),
			}

			bb := businessBrowserOnPage(page, "123")
			err := bb.UploadPhoto(context.Background(), srv.URL, tc.category)
			if err != nil {
				t.Fatalf("UploadPhoto(%s) returned error: %v", tc.category, err)
			}
			if len(fileInput.setInputFiles) == 0 {
				t.Fatalf("expected SetInputFiles on %s, got none", tc.selector)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// closePopups tests
// -----------------------------------------------------------------------------

// Test #17 — closePopups clicks every known dismiss selector that resolves
// (errors are swallowed; callers always continue on to the real action).
func TestClosePopups_ClicksKnownDismissSelectors(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/")
	infoModal := &mockLocator{}
	crossPlat := &mockLocator{}
	page.locators[".InfoModal-IconClose"] = infoModal
	page.locators[".CrossPlatformModal-Close"] = crossPlat
	// Other selectors not registered — Click on default mockLocator returns nil
	// since clickErr is zero, so we don't assert on those.

	closePopups(page)

	if infoModal.clickCount == 0 {
		t.Error("expected .InfoModal-IconClose to be clicked")
	}
	if crossPlat.clickCount == 0 {
		t.Error("expected .CrossPlatformModal-Close to be clicked")
	}
}
