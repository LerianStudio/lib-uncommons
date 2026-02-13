//go:build unit

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cn "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewRequestInfo
// ---------------------------------------------------------------------------

func TestNewRequestInfo_Basic(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	var info *RequestInfo

	app.Get("/api/test", func(c *fiber.Ctx) error {
		info = NewRequestInfo(c)
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(cn.HeaderID, "trace-123")
	req.Header.Set(cn.HeaderUserAgent, "test-agent")

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.NotNil(t, info)
	assert.Equal(t, http.MethodGet, info.Method)
	assert.Equal(t, "/api/test", info.URI)
	assert.Equal(t, "trace-123", info.TraceID)
	assert.Equal(t, "test-agent", info.UserAgent)
	assert.Equal(t, "-", info.Username)
	assert.Equal(t, "-", info.Referer)
	assert.False(t, info.Date.IsZero())
}

func TestNewRequestInfo_WithReferer(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	var info *RequestInfo

	app.Get("/", func(c *fiber.Ctx) error {
		info = NewRequestInfo(c)
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Referer", "https://example.com")

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, "https://example.com", info.Referer)
}

// ---------------------------------------------------------------------------
// CLFString
// ---------------------------------------------------------------------------

func TestCLFString(t *testing.T) {
	t.Parallel()

	info := &RequestInfo{
		RemoteAddress: "192.168.1.1",
		Username:      "admin",
		Protocol:      "http",
		Date:          time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Method:        "POST",
		URI:           "/api/v1/resource",
		Status:        200,
		Size:          1024,
		Referer:       "-",
		UserAgent:     "curl/7.68.0",
	}

	clf := info.CLFString()

	assert.Contains(t, clf, "192.168.1.1")
	assert.Contains(t, clf, "admin")
	assert.Contains(t, clf, `"POST /api/v1/resource"`)
	assert.Contains(t, clf, "200")
	assert.Contains(t, clf, "1024")
	assert.Contains(t, clf, "curl/7.68.0")
}

func TestStringImplementsStringer(t *testing.T) {
	t.Parallel()

	info := &RequestInfo{
		RemoteAddress: "127.0.0.1",
		Username:      "-",
		Protocol:      "http",
		Date:          time.Now(),
		Method:        "GET",
		URI:           "/",
		Referer:       "-",
		UserAgent:     "-",
	}

	assert.Equal(t, info.CLFString(), info.String())
}

// ---------------------------------------------------------------------------
// FinishRequestInfo
// ---------------------------------------------------------------------------

func TestFinishRequestInfo(t *testing.T) {
	t.Parallel()

	info := &RequestInfo{
		Date: time.Now().Add(-100 * time.Millisecond),
	}

	rw := &ResponseMetricsWrapper{
		StatusCode: 201,
		Size:       512,
	}

	info.FinishRequestInfo(rw)

	assert.Equal(t, 201, info.Status)
	assert.Equal(t, 512, info.Size)
	assert.True(t, info.Duration >= 100*time.Millisecond)
}

// ---------------------------------------------------------------------------
// buildOpts / WithCustomLogger
// ---------------------------------------------------------------------------

func TestBuildOpts_Default(t *testing.T) {
	t.Parallel()

	mid := buildOpts()
	assert.NotNil(t, mid.Logger)
	assert.IsType(t, &log.GoLogger{}, mid.Logger)
}

func TestBuildOpts_WithCustomLogger(t *testing.T) {
	t.Parallel()

	custom := &mockLogger{}
	mid := buildOpts(WithCustomLogger(custom))
	assert.Equal(t, custom, mid.Logger)
}

func TestWithCustomLogger_NilDoesNotOverride(t *testing.T) {
	t.Parallel()

	mid := buildOpts(WithCustomLogger(nil))
	assert.NotNil(t, mid.Logger)
	assert.IsType(t, &log.GoLogger{}, mid.Logger)
}

// ---------------------------------------------------------------------------
// Body obfuscation
// ---------------------------------------------------------------------------

func TestHandleJSONBody_SensitiveFields(t *testing.T) {
	t.Parallel()

	input := `{"username":"admin","password":"secret123","email":"a@b.com"}`
	result := handleJSONBody([]byte(input))

	assert.NotContains(t, result, "secret123")
	assert.Contains(t, result, cn.ObfuscatedValue)
	assert.Contains(t, result, "admin")
}

func TestHandleJSONBody_InvalidJSON(t *testing.T) {
	t.Parallel()

	input := `not json`
	result := handleJSONBody([]byte(input))
	assert.Equal(t, input, result)
}

func TestHandleJSONBody_NestedSensitive(t *testing.T) {
	t.Parallel()

	input := `{"user":{"name":"alice","password":"pw"},"items":[{"secret_key":"abc"}]}`
	result := handleJSONBody([]byte(input))

	assert.NotContains(t, result, "pw")
	assert.Contains(t, result, "alice")
}

func TestHandleURLEncodedBody_SensitiveFields(t *testing.T) {
	t.Parallel()

	input := "username=admin&password=secret123&name=test"
	result := handleURLEncodedBody([]byte(input))

	assert.NotContains(t, result, "secret123")
	// ObfuscatedValue gets URL-encoded by url.Values.Encode()
	assert.Contains(t, result, "password=")
	assert.Contains(t, result, "admin")
}

func TestHandleURLEncodedBody_InvalidForm(t *testing.T) {
	t.Parallel()

	input := "%ZZinvalid"
	result := handleURLEncodedBody([]byte(input))
	assert.Equal(t, input, result)
}

// ---------------------------------------------------------------------------
// WithHTTPLogging middleware integration
// ---------------------------------------------------------------------------

func TestWithHTTPLogging_SkipsHealthEndpoint(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(WithHTTPLogging())
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWithHTTPLogging_SetsHeaderID(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(WithHTTPLogging())
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	headerID := resp.Header.Get(cn.HeaderID)
	assert.NotEmpty(t, headerID)
}

func TestWithHTTPLogging_SkipsSwagger(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(WithHTTPLogging())
	app.Get("/swagger/doc.json", func(c *fiber.Ctx) error {
		return c.SendString("{}")
	})

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWithHTTPLogging_PostWithJSONBody(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(WithHTTPLogging())
	app.Post("/api", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusCreated)
	})

	body := strings.NewReader(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// mockLogger for WithCustomLogger tests
// ---------------------------------------------------------------------------

type mockLogger struct{}

func (m *mockLogger) Log(context.Context, log.Level, string, ...log.Field) {}
func (m *mockLogger) With(...log.Field) log.Logger                         { return m }
func (m *mockLogger) WithGroup(string) log.Logger                          { return m }
func (m *mockLogger) Enabled(log.Level) bool                               { return true }
func (m *mockLogger) Sync(context.Context) error                           { return nil }
