package vk_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/agent-vk/internal/vk"
)

// vkResponse wraps a value in the VK API response envelope.
func vkResponse(v interface{}) string {
	b, _ := json.Marshal(map[string]interface{}{"response": v})
	return string(b)
}

// vkError returns a VK-style error JSON.
func vkError(code int, msg string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"error_code": code,
			"error_msg":  msg,
		},
	})
	return string(b)
}

// newMockVKServer creates a mock VK API server that handles all 9+ VK methods.
// The uploadURL parameter is set after creation to point the upload_url back to this server.
func newMockVKServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/method/wall.post", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{"post_id": 12345}))
	})

	mux.HandleFunc("/method/wall.get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{
			"count": 2,
			"items": []map[string]interface{}{
				{
					"id": 1, "text": "Hello", "date": 1700000000,
					"likes":    map[string]interface{}{"count": 5},
					"comments": map[string]interface{}{"count": 2},
					"reposts":  map[string]interface{}{"count": 1},
					"views":    map[string]interface{}{"count": 100},
				},
				{
					"id": 2, "text": "World", "date": 1700000100,
					"likes":    map[string]interface{}{"count": 10},
					"comments": map[string]interface{}{"count": 0},
					"reposts":  map[string]interface{}{"count": 0},
					"views":    map[string]interface{}{"count": 50},
				},
			},
		}))
	})

	mux.HandleFunc("/method/wall.createComment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{
			"comment_id":    777,
			"parents_stack": []int{},
		}))
	})

	mux.HandleFunc("/method/wall.deleteComment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(1))
	})

	mux.HandleFunc("/method/wall.getComments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{
			"count": 1,
			"items": []map[string]interface{}{
				{"id": 1, "text": "Nice!", "date": 1700000000, "from_id": 12345},
			},
		}))
	})

	mux.HandleFunc("/method/groups.getById", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{
			"groups": []map[string]interface{}{
				{
					"id":            123456,
					"name":          "Test Community",
					"screen_name":   "testcommunity",
					"description":   "Test desc",
					"members_count": 1000,
					"status":        "Active",
					"site":          "https://example.com",
					"photo_200":     "https://example.com/photo.jpg",
				},
			},
		}))
	})

	mux.HandleFunc("/method/groups.edit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(1))
	})

	mux.HandleFunc("/method/photos.saveWallPhoto", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse([]map[string]interface{}{
			{"id": 99, "owner_id": -123456},
		}))
	})

	// Create a temporary server to get the URL, then register the upload handlers.
	var srv *httptest.Server

	mux.HandleFunc("/method/photos.getWallUploadServer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{
			"upload_url": srv.URL + "/upload",
		}))
	})

	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart (upload from vksdk)
		_ = r.ParseMultipartForm(10 << 20)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"server": 1, "photo": "[]", "hash": "abc123"}`)
	})

	srv = httptest.NewServer(mux)
	return srv
}

// newErrorServer creates a mock server that always returns a VK API error.
func newErrorServer(code int, msg string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkError(code, msg))
	}))
}

// newTimeoutServer creates a server that immediately closes connections.
func newTimeoutServer() *httptest.Server {
	return httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
}

// newClient creates a VK client pointing at the test server.
func newClient(srv *httptest.Server) *vk.Client {
	return vk.NewWithBaseURL("test-token", srv.URL+"/method/")
}

// --- Tests for existing tools ---

func TestClient_PublishPost_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	postID, err := c.PublishPost("-123456", "Hello")

	require.NoError(t, err)
	assert.Equal(t, int64(12345), postID)
}

func TestClient_UpdateGroupInfo_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	err := c.UpdateGroupInfo("-123456", "New desc")

	require.NoError(t, err)
}

func TestClient_GetComments_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	comments, err := c.GetComments("-123456", 42, 10)

	require.NoError(t, err)
	require.Len(t, comments, 1)
	assert.Equal(t, 1, comments[0]["id"])
	assert.Equal(t, "Nice!", comments[0]["text"])
	assert.Equal(t, 1700000000, comments[0]["date"])
	assert.Equal(t, 12345, comments[0]["from_id"])
	assert.Equal(t, 42, comments[0]["post_id"])
}

// --- Error path tests ---

func TestClient_PermanentError_Code5(t *testing.T) {
	srv := newErrorServer(5, "User authorization failed")
	defer srv.Close()

	c := newClient(srv)
	_, err := c.PublishPost("-123456", "Hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "User authorization failed")
}

func TestClient_RateLimitError_Code6(t *testing.T) {
	srv := newErrorServer(6, "Too many requests per second")
	defer srv.Close()

	c := newClient(srv)
	_, err := c.PublishPost("-123456", "Hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Too many requests")
}

func TestClient_NetworkError(t *testing.T) {
	// Create a listener, get its address, then close it to guarantee connection refused.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	l.Close()

	c := vk.NewWithBaseURL("test-token", "http://"+addr+"/method/")
	_, pubErr := c.PublishPost("-123456", "Hello")

	require.Error(t, pubErr)
}

// --- Tests for Phase 3 new tools ---

func TestClient_PostPhoto_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	// Serve a 1x1 PNG image.
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Minimal 1x1 PNG.
		png1x1 := []byte{
			0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00,
			0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
			0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde,
			0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63,
			0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21,
			0xbc, 0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
			0x42, 0x60, 0x82,
		}
		w.Write(png1x1)
	}))
	defer imgServer.Close()

	c := newClient(srv)
	postID, err := c.PostPhoto("-123456", imgServer.URL+"/image.png", "My caption")

	require.NoError(t, err)
	assert.Equal(t, int64(12345), postID)
}

func TestClient_PostPhoto_InvalidURL(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	_, err := c.PostPhoto("-123456", "http://127.0.0.1:1/nonexistent.png", "caption")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "download image")
}

func TestClient_SchedulePost_Success(t *testing.T) {
	// Create a mock server that verifies publish_date is set.
	var capturedPublishDate string
	mux := http.NewServeMux()
	mux.HandleFunc("/method/wall.post", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedPublishDate = r.FormValue("publish_date")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vkResponse(map[string]interface{}{"post_id": 54321}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	futureTS := time.Now().Add(24 * time.Hour).Unix()
	c := vk.NewWithBaseURL("test-token", srv.URL+"/method/")
	postID, err := c.SchedulePost("-123456", "Future post", futureTS)

	require.NoError(t, err)
	assert.Equal(t, int64(54321), postID)
	assert.NotEmpty(t, capturedPublishDate, "publish_date should be sent to VK API")
}

func TestClient_ReplyComment_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	commentID, err := c.ReplyComment("-123456", 1, 5, "Thanks!")

	require.NoError(t, err)
	assert.Equal(t, 777, commentID)
}

func TestClient_DeleteComment_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	err := c.DeleteComment("-123456", 5)

	require.NoError(t, err)
}

func TestClient_GetCommunityInfo_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	info, err := c.GetCommunityInfo("-123456")

	require.NoError(t, err)
	assert.Equal(t, "Test Community", info["name"])
	assert.Equal(t, 1000, info["members_count"])
	assert.Equal(t, "Test desc", info["description"])
	assert.Equal(t, "testcommunity", info["screen_name"])
	assert.Equal(t, "Active", info["status"])
	assert.Equal(t, "https://example.com", info["site"])
	assert.Equal(t, "https://example.com/photo.jpg", info["photo"])
}

func TestClient_GetWallPosts_Success(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	posts, total, err := c.GetWallPosts("-123456", 10)

	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, posts, 2)
	assert.Equal(t, 1, posts[0]["id"])
	assert.Equal(t, "Hello", posts[0]["text"])
	assert.Equal(t, 5, posts[0]["likes"])
	assert.Equal(t, 2, posts[0]["comments"])
	assert.Equal(t, 1, posts[0]["reposts"])
	assert.Equal(t, 100, posts[0]["views"])
}

// --- Additional error tests for new tools ---

func TestClient_UpdateGroupInfo_Error(t *testing.T) {
	srv := newErrorServer(5, "User authorization failed")
	defer srv.Close()

	c := newClient(srv)
	err := c.UpdateGroupInfo("-123456", "desc")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "User authorization failed")
}

func TestClient_GetCommunityInfo_Error(t *testing.T) {
	srv := newErrorServer(100, "invalid param")
	defer srv.Close()

	c := newClient(srv)
	_, err := c.GetCommunityInfo("-123456")

	require.Error(t, err)
}

func TestClient_GetWallPosts_Error(t *testing.T) {
	srv := newErrorServer(5, "auth failed")
	defer srv.Close()

	c := newClient(srv)
	_, _, err := c.GetWallPosts("-123456", 10)

	require.Error(t, err)
}

func TestClient_GetComments_Error(t *testing.T) {
	srv := newErrorServer(6, "Too many requests per second")
	defer srv.Close()

	c := newClient(srv)
	_, err := c.GetComments("-123456", 1, 10)

	require.Error(t, err)
}

func TestClient_ReplyComment_Error(t *testing.T) {
	srv := newErrorServer(15, "access denied")
	defer srv.Close()

	c := newClient(srv)
	_, err := c.ReplyComment("-123456", 1, 5, "reply")

	require.Error(t, err)
}

func TestClient_DeleteComment_Error(t *testing.T) {
	srv := newErrorServer(15, "access denied")
	defer srv.Close()

	c := newClient(srv)
	err := c.DeleteComment("-123456", 5)

	require.Error(t, err)
}

// --- Invalid group ID tests ---

func TestClient_PostPhoto_InvalidGroupID(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 0x50, 0x4e, 0x47}) // minimal PNG header
		io.WriteString(w, "fake")
	}))
	defer imgServer.Close()

	c := newClient(srv)
	_, err := c.PostPhoto("not-a-number", imgServer.URL+"/img.png", "cap")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid group_id")
}

func TestClient_UpdateGroupInfo_InvalidGroupID(t *testing.T) {
	srv := newMockVKServer()
	defer srv.Close()

	c := newClient(srv)
	err := c.UpdateGroupInfo("abc", "desc")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid group_id")
}
