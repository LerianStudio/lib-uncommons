package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TenantExtractor extracts tenant ID string from a request context.
type TenantExtractor func(ctx context.Context) string

// IDLocation defines where a resource ID should be extracted from.
type IDLocation string

const (
	IDLocationParam IDLocation = "param"
	IDLocationQuery IDLocation = "query"
)

// ErrInvalidIDLocation indicates an unsupported ID source location.
var ErrInvalidIDLocation = errors.New("invalid id location")

// Sentinel errors for context ownership verification.
var (
	ErrMissingContextID    = errors.New("context ID is required")
	ErrInvalidContextID    = errors.New("context ID must be a valid UUID")
	ErrTenantIDNotFound    = errors.New("tenant ID not found in request context")
	ErrTenantExtractorNil  = errors.New("tenant extractor is not configured")
	ErrInvalidTenantID     = errors.New("invalid tenant ID format")
	ErrContextNotFound     = errors.New("context not found")
	ErrContextNotOwned     = errors.New("context does not belong to the requesting tenant")
	ErrContextAccessDenied = errors.New("access to context denied")
	ErrContextNotActive    = errors.New("context is not active")
	ErrContextLookupFailed = errors.New("context lookup failed")
)

// ErrVerifierNotConfigured indicates that no ownership verifier was provided.
var ErrVerifierNotConfigured = errors.New("ownership verifier is not configured")

// Sentinel errors for general resource ownership verification.
// ErrLookupFailed indicates an ownership lookup failed unexpectedly.
var ErrLookupFailed = errors.New("resource lookup failed")

// Sentinel errors for exception ownership verification.
var (
	ErrMissingExceptionID    = errors.New("exception ID is required")
	ErrInvalidExceptionID    = errors.New("exception ID must be a valid UUID")
	ErrExceptionNotFound     = errors.New("exception not found")
	ErrExceptionAccessDenied = errors.New("access to exception denied")
)

// Sentinel errors for dispute ownership verification.
var (
	ErrMissingDisputeID    = errors.New("dispute ID is required")
	ErrInvalidDisputeID    = errors.New("dispute ID must be a valid UUID")
	ErrDisputeNotFound     = errors.New("dispute not found")
	ErrDisputeAccessDenied = errors.New("access to dispute denied")
)

// TenantOwnershipVerifier validates ownership using tenant and resource IDs.
type TenantOwnershipVerifier func(ctx context.Context, tenantID, resourceID uuid.UUID) error

// ResourceOwnershipVerifier validates ownership using resource ID only.
type ResourceOwnershipVerifier func(ctx context.Context, resourceID uuid.UUID) error

// ParseAndVerifyTenantScopedID extracts and validates tenant + resource IDs.
func ParseAndVerifyTenantScopedID(
	fiberCtx *fiber.Ctx,
	idName string,
	location IDLocation,
	verifier TenantOwnershipVerifier,
	tenantExtractor TenantExtractor,
	missingErr error,
	invalidErr error,
	accessErr error,
) (uuid.UUID, uuid.UUID, error) {
	if fiberCtx == nil {
		return uuid.Nil, uuid.Nil, ErrContextNotFound
	}

	if verifier == nil {
		return uuid.Nil, uuid.Nil, ErrVerifierNotConfigured
	}

	resourceID, ctx, tenantID, err := parseTenantAndResourceID(
		fiberCtx,
		idName,
		location,
		tenantExtractor,
		missingErr,
		invalidErr,
	)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if err := verifier(ctx, tenantID, resourceID); err != nil {
		return uuid.Nil, uuid.Nil, classifyOwnershipError(err, accessErr)
	}

	return resourceID, tenantID, nil
}

// ParseAndVerifyResourceScopedID extracts and validates tenant + resource IDs,
// then verifies resource ownership where tenant is implicit in the verifier.
func ParseAndVerifyResourceScopedID(
	fiberCtx *fiber.Ctx,
	idName string,
	location IDLocation,
	verifier ResourceOwnershipVerifier,
	tenantExtractor TenantExtractor,
	missingErr error,
	invalidErr error,
	accessErr error,
	verificationLabel string,
) (uuid.UUID, uuid.UUID, error) {
	if fiberCtx == nil {
		return uuid.Nil, uuid.Nil, ErrContextNotFound
	}

	if verifier == nil {
		return uuid.Nil, uuid.Nil, ErrVerifierNotConfigured
	}

	resourceID, ctx, tenantID, err := parseTenantAndResourceID(
		fiberCtx,
		idName,
		location,
		tenantExtractor,
		missingErr,
		invalidErr,
	)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if err := verifier(ctx, resourceID); err != nil {
		return uuid.Nil, uuid.Nil, classifyResourceOwnershipError(verificationLabel, err, accessErr)
	}

	return resourceID, tenantID, nil
}

func parseTenantAndResourceID(
	fiberCtx *fiber.Ctx,
	idName string,
	location IDLocation,
	tenantExtractor TenantExtractor,
	missingErr error,
	invalidErr error,
) (uuid.UUID, context.Context, uuid.UUID, error) {
	ctx := fiberCtx.UserContext()

	if tenantExtractor == nil {
		return uuid.Nil, ctx, uuid.Nil, ErrTenantExtractorNil
	}

	resourceIDStr, err := getIDValue(fiberCtx, idName, location)
	if err != nil {
		return uuid.Nil, ctx, uuid.Nil, err
	}

	if resourceIDStr == "" {
		return uuid.Nil, ctx, uuid.Nil, missingErr
	}

	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		return uuid.Nil, ctx, uuid.Nil, fmt.Errorf("%w: %s", invalidErr, resourceIDStr)
	}

	tenantIDStr := tenantExtractor(ctx)
	if tenantIDStr == "" {
		return uuid.Nil, ctx, uuid.Nil, ErrTenantIDNotFound
	}

	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return uuid.Nil, ctx, uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidTenantID, err)
	}

	return resourceID, ctx, tenantID, nil
}

func getIDValue(fiberCtx *fiber.Ctx, idName string, location IDLocation) (string, error) {
	if fiberCtx == nil {
		return "", ErrContextNotFound
	}

	switch location {
	case IDLocationParam:
		return fiberCtx.Params(idName), nil
	case IDLocationQuery:
		return fiberCtx.Query(idName), nil
	default:
		return "", ErrInvalidIDLocation
	}
}

func classifyOwnershipError(err, accessErr error) error {
	switch {
	case errors.Is(err, ErrContextNotFound):
		return ErrContextNotFound
	case errors.Is(err, ErrContextNotOwned):
		if accessErr != nil {
			return accessErr
		}

		return ErrContextNotOwned
	case errors.Is(err, ErrContextNotActive):
		return ErrContextNotActive
	case errors.Is(err, ErrContextAccessDenied):
		if accessErr != nil {
			return accessErr
		}

		return ErrContextAccessDenied
	default:
		return fmt.Errorf("%w: %w", ErrContextLookupFailed, err)
	}
}

func classifyResourceOwnershipError(label string, err, accessErr error) error {
	switch {
	case errors.Is(err, ErrExceptionNotFound),
		errors.Is(err, ErrDisputeNotFound):
		return err
	case errors.Is(err, ErrExceptionAccessDenied),
		errors.Is(err, ErrDisputeAccessDenied):
		if accessErr != nil {
			return accessErr
		}

		return err
	default:
		return fmt.Errorf("%s %w: %w", label, ErrLookupFailed, err)
	}
}

// SetHandlerSpanAttributes adds tenant_id and context_id attributes to a trace span.
func SetHandlerSpanAttributes(span trace.Span, tenantID, contextID uuid.UUID) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String("tenant.id", tenantID.String()))

	if contextID != uuid.Nil {
		span.SetAttributes(attribute.String("context.id", contextID.String()))
	}
}

// SetTenantSpanAttribute adds tenant_id attribute to a trace span.
func SetTenantSpanAttribute(span trace.Span, tenantID uuid.UUID) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String("tenant.id", tenantID.String()))
}

// SetExceptionSpanAttributes adds tenant_id and exception_id attributes to a trace span.
func SetExceptionSpanAttributes(span trace.Span, tenantID, exceptionID uuid.UUID) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String("tenant.id", tenantID.String()))
	span.SetAttributes(attribute.String("exception.id", exceptionID.String()))
}

// SetDisputeSpanAttributes adds tenant_id and dispute_id attributes to a trace span.
func SetDisputeSpanAttributes(span trace.Span, tenantID, disputeID uuid.UUID) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String("tenant.id", tenantID.String()))
	span.SetAttributes(attribute.String("dispute.id", disputeID.String()))
}
