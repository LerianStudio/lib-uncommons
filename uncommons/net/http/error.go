// Package http provides shared HTTP helpers.
package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// WriteError writes a structured error response using the ErrorResponse schema.
// This is the canonical way to write error responses and ensures consistency
// across all handlers.
func WriteError(c *fiber.Ctx, status int, title, message string) error {
	return JSONResponse(c, status, ErrorResponse{
		Code:    strconv.Itoa(status),
		Title:   title,
		Message: message,
		Error:   message, // Backward compatibility
	})
}

// BadRequestError writes a 400 Bad Request error response.
func BadRequestError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusBadRequest, title, message)
}

// UnauthorizedError writes a 401 Unauthorized error response.
func UnauthorizedError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusUnauthorized, title, message)
}

// ForbiddenError writes a 403 Forbidden error response.
func ForbiddenError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusForbidden, title, message)
}

// NotFoundError writes a 404 Not Found error response.
func NotFoundError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusNotFound, title, message)
}

// ConflictError writes a 409 Conflict error response.
func ConflictError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusConflict, title, message)
}

// RequestEntityTooLargeError writes a 413 Request Entity Too Large error response.
func RequestEntityTooLargeError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusRequestEntityTooLarge, title, message)
}

// UnprocessableEntityError writes a 422 Unprocessable Entity error response.
func UnprocessableEntityError(c *fiber.Ctx, title, message string) error {
	return WriteError(c, fiber.StatusUnprocessableEntity, title, message)
}

// SimpleInternalServerError writes a 500 Internal Server Error response.
// It always returns a generic message to avoid leaking internal details.
// NOTE: Named SimpleInternalServerError to avoid collision with the existing
// InternalServerError(c, code, title, message) in response.go.
func SimpleInternalServerError(c *fiber.Ctx) error {
	return WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
}

// InternalServerErrorWithTitle writes a 500 Internal Server Error response with a custom title.
// The message is kept generic to avoid leaking internal details.
func InternalServerErrorWithTitle(c *fiber.Ctx, title string) error {
	return WriteError(c, fiber.StatusInternalServerError, title, "internal server error")
}

// ServiceUnavailableError writes a 503 Service Unavailable response.
// It always returns a generic message to avoid leaking internal details.
func ServiceUnavailableError(c *fiber.Ctx) error {
	return WriteError(c, fiber.StatusServiceUnavailable, "service_unavailable", "service unavailable")
}

// ServiceUnavailableErrorWithTitle writes a 503 Service Unavailable response with a custom title.
// The message is kept generic to avoid leaking internal details.
func ServiceUnavailableErrorWithTitle(c *fiber.Ctx, title string) error {
	return WriteError(c, fiber.StatusServiceUnavailable, title, "service unavailable")
}

// GatewayTimeoutError writes a 504 Gateway Timeout response.
// It always returns a generic message to avoid leaking internal details.
func GatewayTimeoutError(c *fiber.Ctx) error {
	return WriteError(c, fiber.StatusGatewayTimeout, "gateway_timeout", "gateway timeout")
}

// GatewayTimeoutErrorWithTitle writes a 504 Gateway Timeout response with a custom title.
// The message is kept generic to avoid leaking internal details.
func GatewayTimeoutErrorWithTitle(c *fiber.Ctx, title string) error {
	return WriteError(c, fiber.StatusGatewayTimeout, title, "gateway timeout")
}
