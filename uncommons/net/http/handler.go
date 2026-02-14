package http

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons"
	cn "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	libLog "github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	libOpentelemetry "github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"
)

// Ping returns HTTP Status 200 with response "pong".
func Ping(c *fiber.Ctx) error {
	return c.SendString("pong")
}

// Version returns HTTP Status 200 with given version.
func Version(c *fiber.Ctx) error {
	return Respond(c, fiber.StatusOK, fiber.Map{
		"version":     uncommons.GetenvOrDefault("VERSION", "0.0.0"),
		"requestDate": time.Now().UTC(),
	})
}

// Welcome returns HTTP Status 200 with service info.
func Welcome(service string, description string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service":     service,
			"description": description,
		})
	}
}

// NotImplementedEndpoint returns HTTP 501 with not implemented message.
func NotImplementedEndpoint(c *fiber.Ctx) error {
	return RespondError(c, fiber.StatusNotImplemented, "not_implemented", "Not implemented yet")
}

// File serves a specific file.
func File(filePath string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.SendFile(filePath)
	}
}

// ExtractTokenFromHeader extracts the authentication token from the Authorization header.
// It handles both "Bearer TOKEN" format and raw token format.
func ExtractTokenFromHeader(c *fiber.Ctx) string {
	authHeader := c.Get(fiber.HeaderAuthorization)

	if authHeader == "" {
		return ""
	}

	splitToken := strings.Split(authHeader, " ")

	if len(splitToken) > 1 && strings.EqualFold(splitToken[0], cn.Bearer) {
		return strings.TrimSpace(splitToken[1])
	}

	if len(splitToken) > 0 {
		return strings.TrimSpace(splitToken[0])
	}

	return ""
}

// FiberErrorHandler is the canonical Fiber error handler.
// It uses the structured logger from the request context so that error
// details pass through the sanitization pipeline instead of going to
// plain stdlib log.Printf.
func FiberErrorHandler(c *fiber.Ctx, err error) error {
	// Safely end spans if user context exists
	ctx := c.UserContext()
	if ctx != nil {
		span := trace.SpanFromContext(ctx)
		libOpentelemetry.HandleSpanError(span, "handler error", err)
		span.End()
	}

	var fe *fiber.Error
	if errors.As(err, &fe) {
		return RenderError(c, ErrorResponse{
			Code:    fe.Code,
			Title:   cn.DefaultErrorTitle,
			Message: fe.Message,
		})
	}

	if ctx == nil {
		ctx = context.Background()
	}

	logger := uncommons.NewLoggerFromContext(ctx)
	logger.Log(ctx, libLog.LevelError,
		"handler error",
		libLog.String("method", c.Method()),
		libLog.String("path", c.Path()),
		libLog.Err(err),
	)

	return RenderError(c, err)
}
