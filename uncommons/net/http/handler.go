package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons"
	libLog "github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"
)

// Ping returns HTTP Status 200 with response "pong".
func Ping(c *fiber.Ctx) error {
	if err := c.SendString("healthy"); err != nil {
		log.Print(err.Error())
	}

	return nil
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

// File servers a specific file.
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

	if len(splitToken) > 1 && strings.EqualFold(splitToken[0], "bearer") {
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
		trace.SpanFromContext(ctx).End()
	}

	var fe *fiber.Error
	if errors.As(err, &fe) {
		return RenderError(c, ErrorResponse{
			Code:    fe.Code,
			Title:   "request_failed",
			Message: fe.Message,
		})
	}

	if ctx == nil {
		ctx = context.Background()
	}

	logger := uncommons.NewLoggerFromContext(ctx)
	logger.Log(ctx, libLog.LevelError,
		fmt.Sprintf("handler error on %s %s", c.Method(), c.Path()),
		libLog.Err(err),
	)

	return RenderError(c, err)
}
