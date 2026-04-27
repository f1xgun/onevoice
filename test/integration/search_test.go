package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Phase 19 / Plan 19-03 — search backend integration tests.
//
// Pattern source: test/integration/authorization_test.go lines 13-194
// (canonical two-user pattern: setupTestUser + setupTestBusiness +
// bearer-token-scoped requests). Conversation + message seeding goes
// through Mongo directly because /api/v1/chat/{id} is an SSE proxy that
// would require the orchestrator to be running.
//
// All tests skip via t.Skip when mongoDB is nil (Mongo unavailable in
// the local dev environment) — preserves CI behavior on environments
// without TEST_MONGO_URL.
//
// THREAT_MODEL: T-19-CROSS-TENANT (HIGH) is the load-bearing test below.
// User A's GET /search?q=инвойс MUST return ONLY User A's conversation,
// even when User B's conversation contains the same Russian-stemmed term.

// resolveBusinessID hits GET /api/v1/business with the bearer and
// extracts the .id from the response. Used to derive (userID, businessID)
// pairs for the direct Mongo conversation seeding helper below.
func resolveBusinessID(t *testing.T, accessToken string) (string, string) {
	t.Helper()

	req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	bizID, _ := body["id"].(string)
	userID, _ := body["userId"].(string)
	require.NotEmpty(t, bizID, "business response must carry id")
	require.NotEmpty(t, userID, "business response must carry userId")
	return userID, bizID
}

// seedConvWithMessage writes one conversation and one user-role message
// directly into Mongo. Returns the conversation ID. Bypasses the SSE
// chat-proxy because we just need the data shape that
// repository.SearchByConversationIDs reads.
func seedConvWithMessage(t *testing.T, userID, businessID, projectID, title, msgContent string) string {
	t.Helper()
	require.NotNil(t, mongoDB, "TEST_MONGO_URL not set — cannot seed conversation")

	ctx := context.Background()
	now := time.Now().UTC()

	convID := primitive.NewObjectID().Hex()
	convDoc := bson.M{
		"_id":             convID,
		"user_id":         userID,
		"business_id":     businessID,
		"title":           title,
		"created_at":      now,
		"updated_at":      now,
		"last_message_at": now,
	}
	if projectID != "" {
		convDoc["project_id"] = projectID
	} else {
		convDoc["project_id"] = nil
	}
	_, err := mongoDB.Collection("conversations").InsertOne(ctx, convDoc)
	require.NoError(t, err)

	msgID := primitive.NewObjectID().Hex()
	_, err = mongoDB.Collection("messages").InsertOne(ctx, bson.M{
		"_id":             msgID,
		"conversation_id": convID,
		"role":            "user",
		"content":         msgContent,
		"created_at":      now,
	})
	require.NoError(t, err)
	return convID
}

// ensureSearchIndexes creates the Phase 19 text indexes idempotently
// after cleanupDatabase drops the conversations + messages collections.
// Mirrors services/api/internal/repository/search_indexes.go EnsureSearchIndexes
// (mongo-driver v2 there; mongo-driver v1 here — same IndexModel shape).
func ensureSearchIndexes(t *testing.T) {
	t.Helper()
	require.NotNil(t, mongoDB, "TEST_MONGO_URL not set — cannot create indexes")

	ctx := context.Background()

	titleIdx := mongo.IndexModel{
		Keys: bson.D{{Key: "title", Value: "text"}},
		Options: options.Index().
			SetName("conversations_title_text_v19").
			SetDefaultLanguage("russian").
			SetWeights(bson.M{"title": 20}),
	}
	if _, err := mongoDB.Collection("conversations").Indexes().CreateOne(ctx, titleIdx); err != nil {
		// Idempotent — duplicate-key / "already exists" is fine
		t.Logf("title index already present or non-fatal: %v", err)
	}

	contentIdx := mongo.IndexModel{
		Keys: bson.D{{Key: "content", Value: "text"}},
		Options: options.Index().
			SetName("messages_content_text_v19").
			SetDefaultLanguage("russian").
			SetWeights(bson.M{"content": 10}),
	}
	if _, err := mongoDB.Collection("messages").Indexes().CreateOne(ctx, contentIdx); err != nil {
		t.Logf("content index already present or non-fatal: %v", err)
	}
}

// doSearch issues GET /api/v1/search with the given query/projectID
// scope and returns the parsed []SearchResult body + status code.
func doSearch(t *testing.T, accessToken, q, projectID string) (int, []map[string]interface{}, http.Header) {
	t.Helper()
	v := url.Values{}
	v.Set("q", q)
	if projectID != "" {
		v.Set("project_id", projectID)
	}
	v.Set("limit", "20")

	u := baseURL + "/api/v1/search?" + v.Encode()
	req, _ := http.NewRequest("GET", u, nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, resp.Header
	}
	var rows []map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	return resp.StatusCode, rows, resp.Header
}

// TestSearchCrossTenant — BLOCKING acceptance for T-19-CROSS-TENANT.
//
// Two users each create a conversation containing the literal Russian
// term «инвойс». User A's GET /search?q=инвойс MUST return ONLY User A's
// conversation; User B's must NOT appear. Symmetric assertion for
// User B. Mirrors authorization_test.go's two-user shape.
func TestSearchCrossTenant(t *testing.T) {
	if mongoDB == nil {
		t.Skip("TEST_MONGO_URL not set — search integration test requires direct Mongo access for seeding")
	}
	cleanupDatabase(t)
	ensureSearchIndexes(t)

	accessTokenA := setupTestUser(t, "userA-search@example.com", "password123")
	accessTokenB := setupTestUser(t, "userB-search@example.com", "password123")
	setupTestBusiness(t, accessTokenA)
	setupTestBusiness(t, accessTokenB)

	userA, bizA := resolveBusinessID(t, accessTokenA)
	userB, bizB := resolveBusinessID(t, accessTokenB)

	convA := seedConvWithMessage(t, userA, bizA, "", "Тест A", "Пожалуйста, выпиши инвойс на услугу")
	convB := seedConvWithMessage(t, userB, bizB, "", "Тест B", "Можно инвойс отправить?")

	t.Run("UserASearchesInvoiceSeesOnlyOwn", func(t *testing.T) {
		status, rows, _ := doSearch(t, accessTokenA, "инвойс", "")
		require.Equal(t, http.StatusOK, status)

		var sawOwn bool
		for _, r := range rows {
			id, _ := r["conversationId"].(string)
			assert.NotEqual(t, convB, id,
				"User A's search MUST NOT return Business B's conversation")
			if id == convA {
				sawOwn = true
			}
		}
		assert.True(t, sawOwn, "User A should see their own conversation")
	})

	t.Run("UserBSearchesInvoiceSeesOnlyOwn", func(t *testing.T) {
		status, rows, _ := doSearch(t, accessTokenB, "инвойс", "")
		require.Equal(t, http.StatusOK, status)

		var sawOwn bool
		for _, r := range rows {
			id, _ := r["conversationId"].(string)
			assert.NotEqual(t, convA, id,
				"User B's search MUST NOT return Business A's conversation")
			if id == convB {
				sawOwn = true
			}
		}
		assert.True(t, sawOwn, "User B should see their own conversation")
	})
}

// TestSearchEmptyQueryReturns400 — q < 2 chars → 400 (D-13).
func TestSearchEmptyQueryReturns400(t *testing.T) {
	if mongoDB == nil {
		t.Skip("TEST_MONGO_URL not set")
	}
	cleanupDatabase(t)
	accessToken := setupTestUser(t, "userShortQ@example.com", "password123")
	setupTestBusiness(t, accessToken)

	for _, q := range []string{"", "a"} {
		t.Run(fmt.Sprintf("q=%q", q), func(t *testing.T) {
			status, _, _ := doSearch(t, accessToken, q, "")
			assert.Equal(t, http.StatusBadRequest, status)
		})
	}
}

// TestSearchMissingBearerReturns401 — without Authorization header,
// the auth middleware rejects.
func TestSearchMissingBearerReturns401(t *testing.T) {
	if mongoDB == nil {
		t.Skip("TEST_MONGO_URL not set")
	}
	cleanupDatabase(t)

	status, _, _ := doSearch(t, "" /* no token */, "инвойс", "")
	assert.Equal(t, http.StatusUnauthorized, status)
}

// TestSearch_503BeforeReady — readiness flag is process-internal and
// flips synchronously at startup; an integration test that boots a
// fresh API would be needed to exercise the cold-boot 503. The unit
// test in services/api/internal/handler/search_test.go::
// TestSearchHandler_503BeforeReady covers the contract directly.
func TestSearch_503BeforeReady(t *testing.T) {
	t.Skip("readiness flag flips at startup; covered by handler unit test TestSearchHandler_503BeforeReady")
}

// TestSearchAggregatedShape — response is one row per CONVERSATION
// (keyed by conversationId), not raw messages. Seeds 3 messages in one
// conversation containing the search term and asserts exactly one row
// in the response with matchCount == 3.
func TestSearchAggregatedShape(t *testing.T) {
	if mongoDB == nil {
		t.Skip("TEST_MONGO_URL not set")
	}
	cleanupDatabase(t)
	ensureSearchIndexes(t)
	accessToken := setupTestUser(t, "userAgg@example.com", "password123")
	setupTestBusiness(t, accessToken)
	userID, bizID := resolveBusinessID(t, accessToken)

	convID := seedConvWithMessage(t, userID, bizID, "", "Документы", "первое упоминание договор")
	// Add two more matching messages in the same conversation.
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, err := mongoDB.Collection("messages").InsertOne(ctx, bson.M{
			"_id":             primitive.NewObjectID().Hex(),
			"conversation_id": convID,
			"role":            "user",
			"content":         fmt.Sprintf("ещё одно упоминание договор #%d", i),
			"created_at":      time.Now(),
		})
		require.NoError(t, err)
	}

	status, rows, _ := doSearch(t, accessToken, "договор", "")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, rows, 1, "must aggregate to ONE row per conversation, not per message")
	row := rows[0]
	assert.Equal(t, convID, row["conversationId"])
	mc, _ := row["matchCount"].(float64)
	assert.Equal(t, 3.0, mc, "matchCount must equal the number of matching messages in the conversation")
}

// TestSearchProjectScope — ?project_id=… filters out conversations in
// other projects (SEARCH-05).
func TestSearchProjectScope(t *testing.T) {
	if mongoDB == nil {
		t.Skip("TEST_MONGO_URL not set")
	}
	cleanupDatabase(t)
	ensureSearchIndexes(t)
	accessToken := setupTestUser(t, "userProjScope@example.com", "password123")
	setupTestBusiness(t, accessToken)
	userID, bizID := resolveBusinessID(t, accessToken)

	projTarget := primitive.NewObjectID().Hex()
	projOther := primitive.NewObjectID().Hex()

	convInTarget := seedConvWithMessage(t, userID, bizID, projTarget, "in-target", "договор на поставку")
	convInOther := seedConvWithMessage(t, userID, bizID, projOther, "in-other", "договор на консультацию")

	status, rows, _ := doSearch(t, accessToken, "договор", projTarget)
	require.Equal(t, http.StatusOK, status)

	var sawTarget bool
	for _, r := range rows {
		id, _ := r["conversationId"].(string)
		assert.NotEqual(t, convInOther, id,
			"project_id filter must drop conversations in other projects")
		if id == convInTarget {
			sawTarget = true
		}
	}
	assert.True(t, sawTarget, "must see the conversation in the targeted project")
}
