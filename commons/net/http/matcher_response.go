// Package http provides shared HTTP helpers.
package http

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"

	libCommons "github.com/LerianStudio/lib-commons-v2/v3/commons"
)

// ErrorResponse provides a consistent error structure for API responses.
// @Description Standard error response returned by all API endpoints
type ErrorResponse struct {
	// HTTP status code
	Code string `json:"code"            example:"400"`
	// Error type identifier
	Title string `json:"title"           example:"invalid_request"`
	// Human-readable error message
	Message string `json:"message"         example:"context name is required"`
	// Deprecated: Error message for backward compatibility
	Error string `json:"error,omitempty" example:"context name is required"`
}

// WithError writes an error response using lib-commons response formatting.
// If err is nil, no response is written and nil is returned.
// If err is a libCommons.Response, its code/title/message are used directly.
// Otherwise, a 500 response is returned with the error message for debugging.
func WithError(ctx *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}

	var responseErr libCommons.Response
	if errors.As(err, &responseErr) {
		if respErr := JSONResponseError(ctx, responseErr); respErr != nil {
			return fmt.Errorf("json response error: %w", respErr)
		}

		return nil
	}

	if respErr := JSONResponseError(ctx, libCommons.Response{
		Code:    strconv.Itoa(fiber.StatusInternalServerError),
		Title:   "request_failed",
		Message: "An internal error occurred",
	}); respErr != nil {
		return fmt.Errorf("json response error: %w", respErr)
	}

	return nil
}
