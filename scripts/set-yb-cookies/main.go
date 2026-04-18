package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/crypto"
)

func main() {
	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		log.Fatal("ENCRYPTION_KEY env required")
	}
	pgURL := os.Getenv("DATABASE_URL")
	if pgURL == "" {
		pgURL = "postgres://postgres:postgres@localhost:5432/onevoice?sslmode=disable"
	}
	businessID := os.Getenv("BUSINESS_ID")
	if businessID == "" {
		log.Fatal("BUSINESS_ID env required")
	}
	permalink := os.Getenv("PERMALINK")
	if permalink == "" {
		permalink = "default"
	}

	cookiesJSON := os.Stdin
	data, _ := os.ReadFile("/dev/stdin")
	if len(data) == 0 {
		log.Fatal("pipe cookies JSON to stdin")
	}
	_ = cookiesJSON

	encrypted, err := crypto.Encrypt(data, []byte(encKey))
	if err != nil {
		log.Fatalf("encrypt: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	bizID, _ := uuid.Parse(businessID)
	now := time.Now()

	_, err = pool.Exec(ctx, `
		INSERT INTO integrations (id, business_id, platform, status, encrypted_access_token, external_id, metadata, created_at, updated_at)
		VALUES ($1, $2, 'yandex_business', 'active', $3, $4, $5, $6, $6)
		ON CONFLICT (business_id, platform, external_id) DO UPDATE SET
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			updated_at = EXCLUDED.updated_at
	`, uuid.New(), bizID, encrypted, permalink, fmt.Sprintf(`{"permalink":"%s"}`, permalink), now)
	if err != nil {
		log.Fatalf("insert: %v", err)
	}

	fmt.Println("OK: yandex_business integration saved with cookies")
}
