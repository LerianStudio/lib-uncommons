//go:build unit

package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	constant "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestAllowFullOptionsWithCORS_NilApp(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		AllowFullOptionsWithCORS(nil)
	})
}

func TestWithCORS_UsesEnvironmentConfiguration(t *testing.T) {
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_ORIGIN", "https://example.com"))
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_METHODS", "GET,POST,OPTIONS"))
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_HEADERS", "Authorization,Content-Type"))
	require.NoError(t, os.Setenv("ACCESS_CONTROL_EXPOSE_HEADERS", "X-Trace-ID"))
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_CREDENTIALS", "true"))
	t.Cleanup(func() {
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_ORIGIN"))
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_METHODS"))
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_HEADERS"))
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_EXPOSE_HEADERS"))
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_CREDENTIALS"))
	})

	app := fiber.New()
	app.Use(WithCORS())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set(fiber.HeaderOrigin, "https://example.com")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, http.MethodGet)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "https://example.com", resp.Header.Get(fiber.HeaderAccessControlAllowOrigin))
	require.Equal(t, "true", resp.Header.Get(fiber.HeaderAccessControlAllowCredentials))
	require.Contains(t, resp.Header.Get(fiber.HeaderAccessControlAllowMethods), http.MethodGet)
	require.Contains(t, resp.Header.Get(fiber.HeaderAccessControlAllowHeaders), constant.Authorization)
}

func TestAllowFullOptionsWithCORS_RegistersOptionsRoute(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	AllowFullOptionsWithCORS(app)

	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set(fiber.HeaderOrigin, "https://example.com")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, http.MethodGet)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestWithCORS_ExplicitFalseCredentials(t *testing.T) {
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_CREDENTIALS", "false"))
	t.Cleanup(func() {
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_CREDENTIALS"))
	})

	app := fiber.New()
	app.Use(WithCORS())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set(fiber.HeaderOrigin, "https://example.com")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, http.MethodGet)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "", resp.Header.Get(fiber.HeaderAccessControlAllowCredentials))
}

func TestWithCORS_InvalidAllowCredentialsFallsBackToDefault(t *testing.T) {
	require.NoError(t, os.Setenv("ACCESS_CONTROL_ALLOW_CREDENTIALS", "not-a-bool"))
	t.Cleanup(func() {
		require.NoError(t, os.Unsetenv("ACCESS_CONTROL_ALLOW_CREDENTIALS"))
	})

	app := fiber.New()
	app.Use(WithCORS())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set(fiber.HeaderOrigin, "https://example.com")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, http.MethodGet)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "", resp.Header.Get(fiber.HeaderAccessControlAllowCredentials))
}
