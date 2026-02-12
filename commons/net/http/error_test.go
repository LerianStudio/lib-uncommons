//go:build unit

package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return WriteError(c, fiber.StatusBadRequest, "test_error", "test message")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "400", errResp.Code)
	assert.Equal(t, "test_error", errResp.Title)
	assert.Equal(t, "test message", errResp.Message)
}

func TestBadRequestError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return BadRequestError(c, "invalid_input", "name is required")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "400", errResp.Code)
	assert.Equal(t, "invalid_input", errResp.Title)
	assert.Equal(t, "name is required", errResp.Message)
}

func TestUnauthorizedError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return UnauthorizedError(c, "unauthorized", "invalid token")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "401", errResp.Code)
	assert.Equal(t, "unauthorized", errResp.Title)
	assert.Equal(t, "invalid token", errResp.Message)
}

func TestForbiddenError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return ForbiddenError(c, "forbidden", "access denied")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "403", errResp.Code)
	assert.Equal(t, "forbidden", errResp.Title)
	assert.Equal(t, "access denied", errResp.Message)
}

func TestNotFoundError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return NotFoundError(c, "not_found", "resource not found")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "404", errResp.Code)
	assert.Equal(t, "not_found", errResp.Title)
	assert.Equal(t, "resource not found", errResp.Message)
}

func TestConflictError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return ConflictError(c, "conflict", "resource already exists")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "409", errResp.Code)
	assert.Equal(t, "conflict", errResp.Title)
	assert.Equal(t, "resource already exists", errResp.Message)
}

func TestRequestEntityTooLargeError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return RequestEntityTooLargeError(c, "payload_too_large", "file exceeds 100MB limit")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "413", errResp.Code)
	assert.Equal(t, "payload_too_large", errResp.Title)
	assert.Equal(t, "file exceeds 100MB limit", errResp.Message)
}

func TestSimpleInternalServerError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", SimpleInternalServerError)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "500", errResp.Code)
	assert.Equal(t, "internal_error", errResp.Title)
	assert.Equal(t, "internal server error", errResp.Message)
}

func TestInternalServerErrorWithTitle(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return InternalServerErrorWithTitle(c, "database_error")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "500", errResp.Code)
	assert.Equal(t, "database_error", errResp.Title)
	assert.Equal(t, "internal server error", errResp.Message)
}

func TestServiceUnavailableError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", ServiceUnavailableError)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusServiceUnavailable, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "503", errResp.Code)
	assert.Equal(t, "service_unavailable", errResp.Title)
	assert.Equal(t, "service unavailable", errResp.Message)
}

func TestServiceUnavailableErrorWithTitle(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return ServiceUnavailableErrorWithTitle(c, "redis_unavailable")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusServiceUnavailable, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "503", errResp.Code)
	assert.Equal(t, "redis_unavailable", errResp.Title)
	assert.Equal(t, "service unavailable", errResp.Message)
}

func TestGatewayTimeoutError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", GatewayTimeoutError)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusGatewayTimeout, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "504", errResp.Code)
	assert.Equal(t, "gateway_timeout", errResp.Title)
	assert.Equal(t, "gateway timeout", errResp.Message)
}

func TestGatewayTimeoutErrorWithTitle(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return GatewayTimeoutErrorWithTitle(c, "upstream_timeout")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusGatewayTimeout, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "504", errResp.Code)
	assert.Equal(t, "upstream_timeout", errResp.Title)
	assert.Equal(t, "gateway timeout", errResp.Message)
}

func TestUnprocessableEntityError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return UnprocessableEntityError(c, "validation_failed", "invalid data format")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnprocessableEntity, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(t, "422", errResp.Code)
	assert.Equal(t, "validation_failed", errResp.Title)
	assert.Equal(t, "invalid data format", errResp.Message)
}

func TestErrorResponseBackwardCompatibility(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return WriteError(c, fiber.StatusBadRequest, "test_title", "test message")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(body, &errResp))

	assert.Equal(
		t,
		errResp.Message,
		errResp.Error,
		"Error field should equal Message for backward compatibility",
	)
}
