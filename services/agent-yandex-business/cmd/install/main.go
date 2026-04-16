package main

import (
	"log"

	"github.com/playwright-community/playwright-go"
)

func main() {
	err := playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}})
	if err != nil {
		log.Fatalf("playwright install failed: %v", err)
	}
	log.Println("playwright driver installed successfully")
}
