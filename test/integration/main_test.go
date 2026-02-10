package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	baseURL     string
	httpClient  *http.Client
	pgPool      *pgxpool.Pool
	mongoDB     *mongo.Database
	redisClient *redis.Client
)

func TestMain(m *testing.M) {
	// Setup
	ctx := context.Background()

	// Get test database URLs from environment
	baseURL = os.Getenv("TEST_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	// Wait for API to be ready
	if err := waitForAPI(baseURL, 30*time.Second); err != nil {
		fmt.Printf("API not ready: %v\n", err)
		os.Exit(1)
	}

	// Connect to databases for cleanup
	pgURL := os.Getenv("TEST_POSTGRES_URL")
	if pgURL != "" {
		var err error
		pgPool, err = pgxpool.New(ctx, pgURL)
		if err != nil {
			fmt.Printf("Failed to connect to postgres: %v\n", err)
		}
	}

	mongoURL := os.Getenv("TEST_MONGO_URL")
	if mongoURL != "" {
		mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
		if err != nil {
			fmt.Printf("Failed to connect to mongo: %v\n", err)
		} else {
			mongoDB = mongoClient.Database("onevoice_test")
		}
	}

	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: redisURL})
	}

	// HTTP client with timeout
	httpClient = &http.Client{Timeout: 10 * time.Second}

	// Run tests
	code := m.Run()

	// Cleanup
	if pgPool != nil {
		pgPool.Close()
	}
	if redisClient != nil {
		redisClient.Close()
	}

	os.Exit(code)
}

func waitForAPI(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("API did not become ready within %v", timeout)
}

func cleanupDatabase(t *testing.T) {
	ctx := context.Background()

	// Clean PostgreSQL
	if pgPool != nil {
		_, err := pgPool.Exec(ctx, "TRUNCATE users, businesses, integrations, business_schedules, refresh_tokens CASCADE")
		if err != nil {
			t.Logf("Warning: failed to clean postgres: %v", err)
		}
	}

	// Clean MongoDB
	if mongoDB != nil {
		mongoDB.Collection("conversations").Drop(ctx)
		mongoDB.Collection("messages").Drop(ctx)
	}

	// Clean Redis
	if redisClient != nil {
		redisClient.FlushDB(ctx)
	}
}
