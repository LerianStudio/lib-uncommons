package http

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// ErrorResponse provides a consistent error structure for API responses.
// @Description Standard error response returned by all API endpoints
type ErrorResponse struct {
	// HTTP status code
	Code int `json:"code"            example:"400"`
	// Error type identifier
	Title string `json:"title"           example:"invalid_request"`
	// Human-readable error message
	Message string `json:"message"         example:"context name is required"`
}

// Error allows ErrorResponse to satisfy the error interface.
func (e ErrorResponse) Error() string {
	return e.Message
}

// RenderError writes all transport errors through a single, stable contract.
func RenderError(ctx *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}

	var presp *ErrorResponse
	if errors.As(err, &presp) {
		status := fiber.StatusInternalServerError

		if presp.Code >= http.StatusContinue && presp.Code <= 599 {
			status = presp.Code
		}

		title := presp.Title
		if title == "" {
			title = "request_failed"
		}

		message := presp.Message
		if message == "" {
			message = http.StatusText(status)
		}

		return RespondError(ctx, status, title, message)
	}

	var responseErr ErrorResponse
	if errors.As(err, &responseErr) {
		status := fiber.StatusInternalServerError

		if responseErr.Code >= http.StatusContinue && responseErr.Code <= 599 {
			status = responseErr.Code
		}

		title := responseErr.Title
		if title == "" {
			title = "request_failed"
		}

		message := responseErr.Message
		if message == "" {
			message = http.StatusText(status)
		}

		return RespondError(ctx, status, title, message)
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return RespondError(ctx, fiberErr.Code, "request_failed", fiberErr.Message)
	}

	return RespondError(ctx, fiber.StatusInternalServerError, "request_failed", "An internal error occurred")
}
