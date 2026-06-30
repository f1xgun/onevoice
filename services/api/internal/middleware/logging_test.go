package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/logger"
)

func TestRequestLogger_Success(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	logOutput := buf.String()

	assert.Contains(t, logOutput, "request started")
	assert.Contains(t, logOutput, "request completed")
	assert.Contains(t, logOutput, `"method":"GET"`)
	assert.Contains(t, logOutput, `"path":"/api/v1/businesses"`)
	assert.Contains(t, logOutput, `"status":200`)
	assert.Contains(t, logOutput, `"remote_addr":"192.168.1.1:12345"`)
	assert.Contains(t, logOutput, "duration_ms")
}

func TestRequestLogger_ErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/unknown", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"status":404`)
}

func TestRequestLogger_ServerErrorLogsAtError(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	assert.Contains(t, out, `"level":"ERROR"`, "5xx access line must log at Error")
	assert.Contains(t, out, `"status":500`)
}

func TestRequestLogger_ClientErrorStaysInfo(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/api/v1/unknown", http.NoBody)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	assert.NotContains(t, out, `"level":"ERROR"`, "4xx is routine — must not log at Error")
	assert.Contains(t, out, `"status":404`)
}

func TestRequestLogger_InjectsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	ctxLogger := slog.New(logger.NewContextHandler(slog.NewJSONHandler(&buf, nil)))

	handler := RequestLogger(ctxLogger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const corrID = "corr-abc-789"
	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req = req.WithContext(logger.WithCorrelationID(req.Context(), corrID))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Contains(t, buf.String(), `"correlation_id":"`+corrID+`"`,
		"the *Context log variants must let the ContextHandler inject correlation_id")
}

func TestRequestLogger_PostRequest(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))

	req := httptest.NewRequest("POST", "/api/v1/businesses", strings.NewReader(`{"name":"test"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"method":"POST"`)
	assert.Contains(t, logOutput, `"status":201`)
}

func TestRequestLogger_ImplicitStatusOK(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"status":200`)
}

func TestResponseWriter_WriteHeaderOnce(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapped := wrapResponseWriter(rr)

	wrapped.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapped.status)

	wrapped.WriteHeader(http.StatusInternalServerError)
	assert.Equal(t, http.StatusCreated, wrapped.status, "status should not change after first WriteHeader")
}

func TestResponseWriter_WriteWithoutWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapped := wrapResponseWriter(rr)

	n, err := wrapped.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, http.StatusOK, wrapped.status)
}

// --- : / PITFALLS §1.4 query-string scrubbing -----------

// TestRequestLogger_StripsTokenFromConfirmPath asserts the access-log
// belt-and-suspenders defense: requests to /auth/password-reset/confirm
// must NEVER leak the ?token=… query string downstream — neither in
// this logger's own output nor via the mutated r.URL.RawQuery the next
// handler in the chain sees.
func TestRequestLogger_StripsTokenFromConfirmPath(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	var rawQueryAtHandlerTime string
	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQueryAtHandlerTime = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm?token=secret123", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.NotContains(t, buf.String(), "secret123",
		"access log must not surface the ?token=… fragment")
	assert.Equal(t, "", rawQueryAtHandlerTime,
		"downstream handler must observe an empty RawQuery for the confirm path")
}

// TestRequestLogger_PassThroughRawQueryForOtherPaths asserts the scrub
// is path-scoped — unrelated requests' query strings stay intact for
// the downstream handler chain (e.g. cursor pagination on /businesses).
func TestRequestLogger_PassThroughRawQueryForOtherPaths(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, nil))

	var observedRawQuery string
	handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/businesses?cursor=abc", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "cursor=abc", observedRawQuery,
		"non-reset paths must preserve their query string for downstream handlers")
}

// TestRequestLogger_RedactsInvitationToken asserts the raw invitation token —
// a bearer credential carried as a URL path segment on the public Preview and
// Accept routes — never reaches the access log, while the downstream handler
// still observes the real token on r.URL.Path.
func TestRequestLogger_RedactsInvitationToken(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"preview", "GET", "/api/v1/invitations/SECRETTOKEN123"},
		{"accept", "POST", "/api/v1/invitations/SECRETTOKEN123/accept"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			lg := slog.New(slog.NewJSONHandler(&buf, nil))

			var pathAtHandlerTime string
			handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pathAtHandlerTime = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.NotContains(t, buf.String(), "SECRETTOKEN123",
				"access log must not surface the raw invitation token path segment")
			assert.Contains(t, buf.String(), "<redacted>",
				"the token segment must be replaced with the redaction marker")
			assert.Equal(t, tc.path, pathAtHandlerTime,
				"downstream handler must still receive the real token on r.URL.Path")
		})
	}
}

// TestRequestLogger_PassThroughNonInvitationPaths asserts the redaction is
// scoped to the two invitation routes — unrelated paths (and the prefix-only
// list/create endpoint) are logged verbatim.
func TestRequestLogger_PassThroughNonInvitationPaths(t *testing.T) {
	paths := []string{
		"/api/v1/businesses",
		"/api/v1/invitations",
		"/api/v1/invitations/",
		"/api/v1/businesses/abc/invitations/def",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			var buf bytes.Buffer
			lg := slog.New(slog.NewJSONHandler(&buf, nil))

			handler := RequestLogger(lg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", p, http.NoBody)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			assert.Contains(t, buf.String(), `"path":"`+p+`"`,
				"non-invitation paths must be logged verbatim")
			assert.NotContains(t, buf.String(), "<redacted>")
		})
	}
}
