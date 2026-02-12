//go:build unit

// Package http provides shared HTTP helpers.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	libCommons "github.com/LerianStudio/lib-commons-v2/v3/commons"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTestDBConnectionFailed = errors.New("database connection failed")
	errTestInternalError      = errors.New("some internal error")
)

func TestWithError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantCode     int
		wantContains string
	}{
		{
			name: "libCommons.Response error uses its code",
			err: libCommons.Response{
				Code:    "400",
				Title:   "Bad Request",
				Message: "Invalid input data",
			},
			wantCode:     fiber.StatusBadRequest,
			wantContains: "Invalid input data",
		},
		{
			name: "libCommons.Response with 404 status",
			err: libCommons.Response{
				Code:    "404",
				Title:   "Not Found",
				Message: "Resource not found",
			},
			wantCode:     fiber.StatusNotFound,
			wantContains: "Resource not found",
		},
		{
			name:         "generic error returns 500 with sanitized message",
			err:          errTestDBConnectionFailed,
			wantCode:     fiber.StatusInternalServerError,
			wantContains: "An internal error occurred",
		},
		{
			name:         "other generic error returns 500 with sanitized message",
			err:          errTestInternalError,
			wantCode:     fiber.StatusInternalServerError,
			wantContains: "An internal error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()
			app.Get("/test", func(c *fiber.Ctx) error {
				return WithError(c, tt.err)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			assert.Equal(t, tt.wantCode, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), tt.wantContains)
		})
	}
}

func TestWithError_NilError(t *testing.T) {
	t.Parallel()

	app := fiber.New()

	var handlerResult error

	app.Get("/test", func(c *fiber.Ctx) error {
		handlerResult = WithError(c, nil)
		return handlerResult
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	assert.NoError(t, handlerResult)
}

func TestWithError_GenericErrorSanitizesMessage(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return WithError(c, errors.New("validation timeout exceeded"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]any

	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	assert.Equal(t, "request_failed", result["title"])
	assert.Equal(t, "An internal error occurred", result["message"])
}

func TestWithError_ResponseFormat(t *testing.T) {
	t.Parallel()

	t.Run("response contains expected JSON structure", func(t *testing.T) {
		t.Parallel()

		app := fiber.New()
		app.Get("/test", func(c *fiber.Ctx) error {
			return WithError(c, libCommons.Response{
				Code:    "422",
				Title:   "Validation Error",
				Message: "Field 'name' is required",
			})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer func() {
			_ = resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(body, &result)
		require.NoError(t, err)

		assert.Equal(t, "422", result["code"])
		assert.Equal(t, "Validation Error", result["title"])
		assert.Equal(t, "Field 'name' is required", result["message"])
	})
}

func TestErrorResponse_Structure(t *testing.T) {
	t.Parallel()

	errResp := ErrorResponse{
		Code:    "400",
		Title:   "bad_request",
		Message: "invalid input",
		Error:   "invalid input",
	}

	assert.Equal(t, "400", errResp.Code)
	assert.Equal(t, "bad_request", errResp.Title)
	assert.Equal(t, "invalid input", errResp.Message)
	assert.Equal(t, "invalid input", errResp.Error)
}
