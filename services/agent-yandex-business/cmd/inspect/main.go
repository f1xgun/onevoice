package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/playwright-community/playwright-go"
)

func main() {
	cookiesJSON := os.Getenv("COOKIES_JSON")
	if cookiesJSON == "" {
		log.Fatal("COOKIES_JSON env required")
	}
	permalink := os.Getenv("PERMALINK")
	if permalink == "" {
		permalink = "114697172504"
	}
	pagePath := os.Getenv("PAGE_PATH")
	if pagePath == "" {
		pagePath = "/p/edit/photos/"
	}
	url := fmt.Sprintf("https://yandex.ru/sprav/%s%s", permalink, pagePath)

	pw, err := playwright.Run()
	if err != nil {
		log.Fatalf("playwright run: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     []string{"--no-sandbox", "--disable-blink-features=AutomationControlled"},
	})
	if err != nil {
		log.Fatalf("launch: %v", err)
	}
	defer browser.Close()

	bCtx, _ := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	})

	var cookies []map[string]interface{}
	json.Unmarshal([]byte(cookiesJSON), &cookies)
	pwCookies := make([]playwright.OptionalCookie, 0)
	for _, c := range cookies {
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		domain, _ := c["domain"].(string)
		path, _ := c["path"].(string)
		pwCookies = append(pwCookies, playwright.OptionalCookie{
			Name: name, Value: value, Domain: playwright.String(domain), Path: playwright.String(path),
		})
	}
	bCtx.AddCookies(pwCookies)

	pg, _ := bCtx.NewPage()
	fmt.Fprintf(os.Stderr, "navigating to %s\n", url)
	pg.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle, Timeout: playwright.Float(30000)})

	// Close popups
	for _, sel := range []string{".InfoModal-IconClose", ".CrossPlatformModal-Close"} {
		pg.Locator(sel).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)})
	}
	time.Sleep(2 * time.Second)

	pg.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("/tmp/inspect_photos.png"), FullPage: playwright.Bool(true)})

	// Find all buttons with text
	buttons, _ := pg.Locator("button").All()
	fmt.Println("=== BUTTONS ===")
	for _, btn := range buttons {
		text, _ := btn.TextContent()
		cls, _ := btn.GetAttribute("class")
		if text != "" {
			fmt.Printf("  text=%q class=%q\n", text[:min(len(text), 60)], cls[:min(len(cls), 80)])
		}
	}

	// Find elements with "Загрузить" text
	fmt.Println("\n=== 'Загрузить' elements ===")
	uploads, _ := pg.Locator(":text('Загрузить')").All()
	for _, u := range uploads {
		tag, _ := u.Evaluate("el => el.tagName", nil)
		cls, _ := u.GetAttribute("class")
		text, _ := u.TextContent()
		fmt.Printf("  tag=%v class=%q text=%q\n", tag, cls, text[:min(len(text), 60)])
	}

	// Find hidden file inputs
	fmt.Println("\n=== input[type=file] ===")
	fileInputs, _ := pg.Locator("input[type='file']").All()
	fmt.Printf("  count=%d\n", len(fileInputs))
	for _, fi := range fileInputs {
		cls, _ := fi.GetAttribute("class")
		accept, _ := fi.GetAttribute("accept")
		name, _ := fi.GetAttribute("name")
		fmt.Printf("  class=%q accept=%q name=%q\n", cls, accept, name)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
