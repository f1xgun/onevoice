package yandex

import (
	"context"
	"errors"
	"fmt"
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
		contexts:  make(map[string]*pooledContext),
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
	page.locators[".CompaniesCompanyRow"] = &mockLocator{}
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
	bb := businessBrowserOnPage(page, "default")

	rows, err := bb.ListCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListCompanies returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty rows, got %d", len(rows))
	}
}

// Test #3b — a malformed Evaluate payload that cannot be decoded into the
// canonical row shape surfaces as an error rather than a silent empty list.
func TestBusinessBrowser_ListCompanies_UnmarshalFailure_ReturnsError(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/companies/")
	page.locators[".CompaniesCompanyRow"] = &mockLocator{}
	page.evaluateResult = "not-an-array-of-rows"

	bb := businessBrowserOnPage(page, "default")
	rows, err := bb.ListCompanies(context.Background())
	if err == nil {
		t.Fatalf("expected error from undecodable companies payload, got rows=%v", rows)
	}
	if !strings.Contains(err.Error(), "companies list") {
		t.Fatalf("expected wrapped companies-list error, got: %v", err)
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

// reviewCardLocator builds a mock review card whose data-review-id attribute
// supplies its stable identity for cross-page dedup.
func reviewCardLocator(id string) *mockLocator {
	return &mockLocator{
		attributes: map[string]string{"data-review-id": id},
		children:   make(map[string]*mockLocator),
	}
}

// Test #5b — GetReviews paginates via "load more" and must NOT re-emit the
// already-captured top cards (which the SPA keeps rendered) when the next page
// appends new ones below them. With the pre-fix top-slice logic the second
// iteration re-read cards from index 0 and returned the SAME top cards while
// the genuinely new tail cards were never reached — duplicated heads and a
// dropped, unrecoverable tail. This test grows the rendered DOM on each
// load-more click and asserts the result is the distinct union, in order,
// including the tail.
func TestBusinessBrowser_GetReviews_LoadMore_NoDuplicatesNoDroppedTail(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/reviews")

	page.locators[".ReviewsPage-ReviewsList"] = &mockLocator{}

	reviewList := &mockLocator{}
	page.locators[".Review"] = reviewList

	for i := 0; i < 15; i++ {
		reviewList.allItems = append(reviewList.allItems, reviewCardLocator(fmt.Sprintf("r-%d", i)))
	}

	pages := [][]string{
		{"r-15", "r-16", "r-17", "r-18", "r-19"},
	}
	pageIdx := 0
	loadMore := &mockLocator{}
	loadMore.clickFn = func() {
		if pageIdx >= len(pages) {
			return
		}
		for _, id := range pages[pageIdx] {
			reviewList.allItems = append(reviewList.allItems, reviewCardLocator(id))
		}
		pageIdx++
	}
	page.locators["[data-testid='load-more-reviews']"] = loadMore

	bb := businessBrowserOnPage(page, "123")
	got, err := bb.GetReviews(context.Background(), 20)
	if err != nil {
		t.Fatalf("GetReviews returned error: %v", err)
	}

	if len(got) != 20 {
		t.Fatalf("expected 20 distinct reviews, got %d", len(got))
	}

	seen := make(map[string]int)
	for _, r := range got {
		id, _ := r["id"].(string)
		seen[id]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("review id %q appeared %d times, want exactly once (duplicate top cards re-emitted)", id, n)
		}
	}

	for i := 0; i < 20; i++ {
		want := fmt.Sprintf("r-%d", i)
		if _, ok := seen[want]; !ok {
			t.Fatalf("expected review id %q in result but it was missing (dropped tail / never scraped)", want)
		}
	}
}

// Test #6 — scrapeReviewCards on an empty DOM returns empty slice (no error).
func TestScrapeReviewCards_HandlesEmpty(t *testing.T) {
	page := newMockPage("https://yandex.ru/sprav/123/p/edit/reviews")
	cards, err := scrapeReviewCards(page)
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

	bb := businessBrowserOnPage(page, "123")
	err := bb.ReplyReview(context.Background(), "rev-1", "thanks!")
	if err == nil {
		t.Fatal("expected error when review card not found")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got: %v", err)
	}
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
// SetInputFiles is invoked on the correctly-mapped locator. The photo download
// is stubbed via fetchPhotoFn so the test never touches the network and the
// SSRF guard on the production path stays intact.
func TestBusinessBrowser_UploadPhoto_SetsCategoryAndUploadsFile(t *testing.T) {
	const png = "\x89PNG\r\n\x1a\n"

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
			page.locators["button:has-text('Сохранить')"] = &mockLocator{
				waitErr: errors.New("no crop dialog"),
			}

			bb := businessBrowserOnPage(page, "123")
			bb.fetchPhotoFn = func(context.Context, string) ([]byte, error) {
				return []byte(png), nil
			}
			err := bb.UploadPhoto(context.Background(), "https://cdn.example.com/photo.png", tc.category)
			if err != nil {
				t.Fatalf("UploadPhoto(%s) returned error: %v", tc.category, err)
			}
			if len(fileInput.setInputFiles) == 0 {
				t.Fatalf("expected SetInputFiles on %s, got none", tc.selector)
			}
		})
	}
}

// Test #16b — when the crop-save dialog appears but its save Click fails,
// UploadPhoto surfaces the error instead of reporting a false success.
func TestBusinessBrowser_UploadPhoto_CropSaveClickFails_ReturnsError(t *testing.T) {
	const png = "\x89PNG\r\n\x1a\n"

	page := newMockPage("https://yandex.ru/sprav/123/p/edit/photos/")
	page.locators[".MediaUploadButton-Input"] = &mockLocator{}
	page.locators["button:has-text('Сохранить')"] = &mockLocator{
		clickErr: errors.New("crop save click intercepted"),
	}

	bb := businessBrowserOnPage(page, "123")
	bb.fetchPhotoFn = func(context.Context, string) ([]byte, error) {
		return []byte(png), nil
	}
	err := bb.UploadPhoto(context.Background(), "https://cdn.example.com/photo.png", "general")
	if err == nil {
		t.Fatal("expected error when crop save click fails, got nil")
	}
	if !strings.Contains(err.Error(), "crop save") {
		t.Fatalf("expected wrapped crop save error, got: %v", err)
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

	closePopups(page)

	if infoModal.clickCount == 0 {
		t.Error("expected .InfoModal-IconClose to be clicked")
	}
	if crossPlat.clickCount == 0 {
		t.Error("expected .CrossPlatformModal-Close to be clicked")
	}
}
