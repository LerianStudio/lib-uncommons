// Package http provides shared HTTP helpers.
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

// TenantExtractor is a function that extracts the tenant ID string from a context.
// Consumers provide their own implementation (e.g., wrapping auth.GetTenantID).
type TenantExtractor func(ctx context.Context) string

// Sentinel errors for context ownership verification.
var (
	// ErrMissingContextID is returned when the context ID parameter is empty.
	ErrMissingContextID = errors.New("context ID is required")

	// ErrInvalidContextID is returned when the context ID is not a valid UUID.
	ErrInvalidContextID = errors.New("context ID must be a valid UUID")

	// ErrTenantIDNotFound is returned when tenant ID cannot be extracted from context.
	ErrTenantIDNotFound = errors.New("tenant ID not found in request context")

	// ErrInvalidTenantID is returned when tenant ID is not a valid UUID.
	ErrInvalidTenantID = errors.New("invalid tenant ID format")

	// ErrContextNotFound is returned when the context does not exist.
	ErrContextNotFound = errors.New("context not found")

	// ErrContextNotOwned is returned when the context does not belong to the tenant.
	ErrContextNotOwned = errors.New("context does not belong to the requesting tenant")

	// ErrContextAccessDenied is returned when access to the context is denied.
	ErrContextAccessDenied = errors.New("access to context denied")

	// ErrContextNotActive is returned when the context exists but is not active.
	ErrContextNotActive = errors.New("context is not active")

	// ErrContextLookupFailed is returned when context lookup fails due to infrastructure errors.
	ErrContextLookupFailed = errors.New("context lookup failed")
)

// Sentinel errors for general resource ownership verification.
var (
	// ErrLookupFailed is returned when a resource ownership lookup fails due to infrastructure errors.
	ErrLookupFailed = errors.New("resource lookup failed")
)

// Sentinel errors for exception ownership verification.
var (
	// ErrMissingExceptionID is returned when the exception ID parameter is empty.
	ErrMissingExceptionID = errors.New("exception ID is required")

	// ErrInvalidExceptionID is returned when the exception ID is not a valid UUID.
	ErrInvalidExceptionID = errors.New("exception ID must be a valid UUID")

	// ErrExceptionNotFound is returned when the exception does not exist in the tenant's schema.
	ErrExceptionNotFound = errors.New("exception not found")

	// ErrExceptionAccessDenied is returned when access to the exception is denied.
	ErrExceptionAccessDenied = errors.New("access to exception denied")
)

// Sentinel errors for dispute ownership verification.
var (
	// ErrMissingDisputeID is returned when the dispute ID parameter is empty.
	ErrMissingDisputeID = errors.New("dispute ID is required")

	// ErrInvalidDisputeID is returned when the dispute ID is not a valid UUID.
	ErrInvalidDisputeID = errors.New("dispute ID must be a valid UUID")

	// ErrDisputeNotFound is returned when the dispute does not exist in the tenant's schema.
	ErrDisputeNotFound = errors.New("dispute not found")

	// ErrDisputeAccessDenied is returned when access to the dispute is denied.
	ErrDisputeAccessDenied = errors.New("access to dispute denied")
)

// ContextOwnershipVerifier defines the interface for verifying that a context
// (such as a file, rule, or reconciliation) belongs to a specific tenant.
type ContextOwnershipVerifier interface {
	// VerifyOwnership checks if the given context ID belongs to the specified tenant.
	// Returns nil if ownership is verified, or an error if verification fails.
	VerifyOwnership(ctx context.Context, tenantID, contextID uuid.UUID) error
}

// ExceptionOwnershipVerifier defines the interface for verifying that an exception
// belongs to a specific tenant's schema.
type ExceptionOwnershipVerifier interface {
	// VerifyOwnership checks if the given exception ID exists in the tenant's schema.
	// Returns nil if the exception exists and belongs to the tenant, or an error if verification fails.
	VerifyOwnership(ctx context.Context, exceptionID uuid.UUID) error
}

// DisputeOwnershipVerifier defines the interface for verifying that a dispute
// belongs to a specific tenant's schema.
type DisputeOwnershipVerifier interface {
	// VerifyOwnership checks if the given dispute ID exists in the tenant's schema.
	// Returns nil if the dispute exists and belongs to the tenant, or an error if verification fails.
	VerifyOwnership(ctx context.Context, disputeID uuid.UUID) error
}

// ParseAndVerifyContextParam extracts the context ID from URL path parameters,
// validates it as a UUID, extracts the tenant ID from the request context,
// and verifies that the context belongs to the tenant.
//
// The contextId is extracted from the URL path parameter named "contextId".
// The tenant ID is extracted from the request context using the provided tenantExtractor.
//
// Returns the parsed context ID, tenant ID, or an error if validation or
// ownership verification fails.
func ParseAndVerifyContextParam(
	fiberCtx *fiber.Ctx,
	verifier ContextOwnershipVerifier,
	tenantExtractor TenantExtractor,
) (uuid.UUID, uuid.UUID, error) {
	if verifier == nil {
		return uuid.Nil, uuid.Nil, ErrContextAccessDenied
	}

	contextIDStr := fiberCtx.Params("contextId")
	if contextIDStr == "" {
		return uuid.Nil, uuid.Nil, ErrMissingContextID
	}

	contextID, err := uuid.Parse(contextIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidContextID, contextIDStr)
	}

	ctx := fiberCtx.UserContext()

	tenantIDStr := tenantExtractor(ctx)
	if tenantIDStr == "" {
		return uuid.Nil, uuid.Nil, ErrTenantIDNotFound
	}

	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidTenantID, err)
	}

	if err := verifier.VerifyOwnership(ctx, tenantID, contextID); err != nil {
		return uuid.Nil, uuid.Nil, classifyOwnershipError(err)
	}

	return contextID, tenantID, nil
}

// ParseAndVerifyContextQuery extracts the context ID from URL query parameters,
// validates it as a UUID, extracts the tenant ID from the request context,
// and verifies that the context belongs to the tenant.
//
// The contextId is extracted from the URL query parameter named "contextId".
// The tenant ID is extracted from the request context using the provided tenantExtractor.
//
// Returns the parsed context ID, tenant ID, or an error if validation or
// ownership verification fails.
func ParseAndVerifyContextQuery(
	fiberCtx *fiber.Ctx,
	verifier ContextOwnershipVerifier,
	tenantExtractor TenantExtractor,
) (uuid.UUID, uuid.UUID, error) {
	if verifier == nil {
		return uuid.Nil, uuid.Nil, ErrContextAccessDenied
	}

	contextIDStr := fiberCtx.Query("contextId")
	if contextIDStr == "" {
		return uuid.Nil, uuid.Nil, ErrMissingContextID
	}

	contextID, err := uuid.Parse(contextIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidContextID, contextIDStr)
	}

	ctx := fiberCtx.UserContext()

	tenantIDStr := tenantExtractor(ctx)
	if tenantIDStr == "" {
		return uuid.Nil, uuid.Nil, ErrTenantIDNotFound
	}

	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidTenantID, err)
	}

	if err := verifier.VerifyOwnership(ctx, tenantID, contextID); err != nil {
		return uuid.Nil, uuid.Nil, classifyOwnershipError(err)
	}

	return contextID, tenantID, nil
}

// SetHandlerSpanAttributes adds tenant_id and context_id attributes to a trace span.
// This should be called after extracting tenant and context information from the request.
// Pass uuid.Nil for contextID if not applicable for the endpoint.
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
// Use this for endpoints that don't have a context ID.
func SetTenantSpanAttribute(span trace.Span, tenantID uuid.UUID) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String("tenant.id", tenantID.String()))
}

// ParseAndVerifyExceptionParam extracts the exception ID from URL path parameters,
// validates it as a UUID, extracts the tenant ID from the request context,
// and verifies that the exception belongs to the tenant's schema.
//
// The exceptionId is extracted from the URL path parameter named "exceptionId".
// The tenant ID is extracted from the request context using the provided tenantExtractor.
//
// Returns the parsed exception ID, tenant ID, or an error if validation or
// ownership verification fails.
func ParseAndVerifyExceptionParam(
	fiberCtx *fiber.Ctx,
	verifier ExceptionOwnershipVerifier,
	tenantExtractor TenantExtractor,
) (uuid.UUID, uuid.UUID, error) {
	return parseAndVerifyParam(
		fiberCtx,
		"exceptionId",
		verifier,
		tenantExtractor,
		ErrMissingExceptionID,
		ErrInvalidExceptionID,
		ErrExceptionAccessDenied,
		"exception",
	)
}

// ParseAndVerifyDisputeParam extracts the dispute ID from URL path parameters,
// validates it as a UUID, extracts the tenant ID from the request context,
// and verifies that the dispute belongs to the tenant's schema.
//
// The disputeId is extracted from the URL path parameter named "disputeId".
// The tenant ID is extracted from the request context using the provided tenantExtractor.
//
// Returns the parsed dispute ID, tenant ID, or an error if validation or
// ownership verification fails.
func ParseAndVerifyDisputeParam(
	fiberCtx *fiber.Ctx,
	verifier DisputeOwnershipVerifier,
	tenantExtractor TenantExtractor,
) (uuid.UUID, uuid.UUID, error) {
	return parseAndVerifyParam(
		fiberCtx,
		"disputeId",
		verifier,
		tenantExtractor,
		ErrMissingDisputeID,
		ErrInvalidDisputeID,
		ErrDisputeAccessDenied,
		"dispute",
	)
}

type ownershipVerifier interface {
	VerifyOwnership(ctx context.Context, resourceID uuid.UUID) error
}

func parseAndVerifyParam(
	fiberCtx *fiber.Ctx,
	paramName string,
	verifier ownershipVerifier,
	tenantExtractor TenantExtractor,
	missingErr error,
	invalidErr error,
	accessErr error,
	verificationLabel string,
) (uuid.UUID, uuid.UUID, error) {
	if verifier == nil {
		return uuid.Nil, uuid.Nil, accessErr
	}

	resourceIDStr := fiberCtx.Params(paramName)
	if resourceIDStr == "" {
		return uuid.Nil, uuid.Nil, missingErr
	}

	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %s", invalidErr, resourceIDStr)
	}

	ctx := fiberCtx.UserContext()

	tenantIDStr := tenantExtractor(ctx)
	if tenantIDStr == "" {
		return uuid.Nil, uuid.Nil, ErrTenantIDNotFound
	}

	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidTenantID, err)
	}

	if err := verifier.VerifyOwnership(ctx, resourceID); err != nil {
		return uuid.Nil, uuid.Nil, classifyResourceOwnershipError(verificationLabel, err)
	}

	return resourceID, tenantID, nil
}

// classifyOwnershipError maps a verifier error to the appropriate sentinel error,
// preventing internal details (e.g., database errors) from leaking to API consumers.
// Known domain errors are preserved; unknown errors are wrapped with ErrContextLookupFailed.
func classifyOwnershipError(err error) error {
	switch {
	case errors.Is(err, ErrContextNotFound):
		return ErrContextNotFound
	case errors.Is(err, ErrContextNotOwned):
		return ErrContextNotOwned
	case errors.Is(err, ErrContextNotActive):
		return ErrContextNotActive
	case errors.Is(err, ErrContextAccessDenied):
		return ErrContextAccessDenied
	default:
		return fmt.Errorf("%w: %w", ErrContextLookupFailed, err)
	}
}

// classifyResourceOwnershipError maps a verifier error to a sanitized error for
// resource ownership verification (exceptions, disputes). Known domain errors
// are preserved; unknown errors are wrapped with a generic lookup failure message.
func classifyResourceOwnershipError(label string, err error) error {
	// Check for known resource-specific sentinel errors
	switch {
	case errors.Is(err, ErrExceptionNotFound),
		errors.Is(err, ErrExceptionAccessDenied),
		errors.Is(err, ErrDisputeNotFound),
		errors.Is(err, ErrDisputeAccessDenied):
		return err
	default:
		return fmt.Errorf("%s %w: %w", label, ErrLookupFailed, err)
	}
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
