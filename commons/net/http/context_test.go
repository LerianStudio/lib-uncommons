//go:build unit

// Package http provides shared HTTP helpers.
package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// testTenantIDKey is a context key used in tests to store tenant ID.
type testTenantIDKey struct{}

// errTestOwnership is a sentinel error for testing ownership verification failures.
var errTestOwnership = errors.New("context not owned by tenant")

// defaultTestTenantID is the default tenant ID used when none is explicitly set in tests.
const defaultTestTenantID = "11111111-1111-1111-1111-111111111111"

// MockContextOwnershipVerifier is a mock implementation of ContextOwnershipVerifier.
type MockContextOwnershipVerifier struct {
	VerifyOwnershipFunc func(ctx context.Context, tenantID, contextID uuid.UUID) error
}

// VerifyOwnership implements ContextOwnershipVerifier.
func (m *MockContextOwnershipVerifier) VerifyOwnership(
	ctx context.Context,
	tenantID, contextID uuid.UUID,
) error {
	if m.VerifyOwnershipFunc != nil {
		return m.VerifyOwnershipFunc(ctx, tenantID, contextID)
	}

	return nil
}

// setupFiberApp creates a Fiber app with the given handler and path parameters.
func setupFiberApp(t *testing.T, handler fiber.Handler, pathPattern string) *fiber.App {
	t.Helper()

	app := fiber.New(fiber.Config{
		ErrorHandler: func(fiberCtx *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError

			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
			}

			return fiberCtx.Status(code).JSON(fiber.Map{
				"error": err.Error(),
				"code":  code,
			})
		},
	})

	app.Get(pathPattern, handler)

	return app
}

// setTenantInContext creates a context with tenant ID set using the test context key.
func setTenantInContext(tenantID string) context.Context {
	return context.WithValue(context.Background(), testTenantIDKey{}, tenantID)
}

// testTenantExtractor returns a TenantExtractor that reads from the test context key.
func testTenantExtractor(ctx context.Context) string {
	if tid, ok := ctx.Value(testTenantIDKey{}).(string); ok && tid != "" {
		return tid
	}

	return defaultTestTenantID
}

func TestParseAndVerifyContextParam(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name           string
		contextID      string
		tenantID       string
		verifierFunc   func(ctx context.Context, tenantID, contextID uuid.UUID) error
		expectedErr    error
		expectedErrMsg string
		wantContextID  uuid.UUID
		wantTenantID   uuid.UUID
	}{
		{
			name:          "missing contextId parameter returns ErrMissingContextID",
			contextID:     "",
			tenantID:      validTenantID.String(),
			verifierFunc:  nil,
			expectedErr:   ErrMissingContextID,
			wantContextID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:           "invalid UUID format returns ErrInvalidContextID",
			contextID:      "not-a-valid-uuid",
			tenantID:       validTenantID.String(),
			verifierFunc:   nil,
			expectedErrMsg: "context ID must be a valid UUID",
			wantContextID:  uuid.Nil,
			wantTenantID:   uuid.Nil,
		},
		{
			name:      "ownership verification fails with unknown error returns lookup failed",
			contextID: validContextID.String(),
			tenantID:  validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return errTestOwnership
			},
			expectedErr:   ErrContextLookupFailed,
			wantContextID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:      "ownership verification fails with not found returns ErrContextNotFound",
			contextID: validContextID.String(),
			tenantID:  validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return ErrContextNotFound
			},
			expectedErr:   ErrContextNotFound,
			wantContextID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:      "success case returns contextID, tenantID, nil",
			contextID: validContextID.String(),
			tenantID:  validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return nil
			},
			expectedErr:   nil,
			wantContextID: validContextID,
			wantTenantID:  validTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockVerifier := &MockContextOwnershipVerifier{
				VerifyOwnershipFunc: tt.verifierFunc,
			}

			var gotContextID, gotTenantID uuid.UUID

			var gotErr error

			handler := func(fiberCtx *fiber.Ctx) error {
				// Set tenant in user context
				ctx := setTenantInContext(tt.tenantID)
				fiberCtx.SetUserContext(ctx)

				gotContextID, gotTenantID, gotErr = ParseAndVerifyContextParam(
					fiberCtx,
					mockVerifier,
					testTenantExtractor,
				)
				if gotErr != nil {
					return fiberCtx.Status(fiber.StatusBadRequest).
						JSON(fiber.Map{"error": gotErr.Error()})
				}

				return fiberCtx.JSON(fiber.Map{
					"contextId": gotContextID.String(),
					"tenantId":  gotTenantID.String(),
				})
			}

			// Use optional parameter pattern for testing empty contextId case
			pathPattern := "/contexts/:contextId?"
			app := setupFiberApp(t, handler, pathPattern)

			urlPath := "/contexts/"
			if tt.contextID != "" {
				urlPath += tt.contextID
			}

			req := httptest.NewRequest(http.MethodGet, urlPath, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			// Check the error
			switch {
			case tt.expectedErr != nil:
				assert.ErrorIs(t, gotErr, tt.expectedErr)
			case tt.expectedErrMsg != "":
				require.Error(t, gotErr)
				assert.Contains(t, gotErr.Error(), tt.expectedErrMsg)
			default:
				assert.NoError(t, gotErr)
			}

			// Check returned values
			assert.Equal(t, tt.wantContextID, gotContextID)
			assert.Equal(t, tt.wantTenantID, gotTenantID)
		})
	}
}

func TestParseAndVerifyContextQuery(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name           string
		contextIDQuery string
		tenantID       string
		verifierFunc   func(ctx context.Context, tenantID, contextID uuid.UUID) error
		expectedErr    error
		expectedErrMsg string
		wantContextID  uuid.UUID
		wantTenantID   uuid.UUID
	}{
		{
			name:           "missing contextId query parameter returns ErrMissingContextID",
			contextIDQuery: "",
			tenantID:       validTenantID.String(),
			verifierFunc:   nil,
			expectedErr:    ErrMissingContextID,
			wantContextID:  uuid.Nil,
			wantTenantID:   uuid.Nil,
		},
		{
			name:           "invalid UUID format in query returns ErrInvalidContextID",
			contextIDQuery: "not-a-valid-uuid",
			tenantID:       validTenantID.String(),
			verifierFunc:   nil,
			expectedErrMsg: "context ID must be a valid UUID",
			wantContextID:  uuid.Nil,
			wantTenantID:   uuid.Nil,
		},
		{
			name:           "ownership verification fails with unknown error returns lookup failed",
			contextIDQuery: validContextID.String(),
			tenantID:       validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return errTestOwnership
			},
			expectedErr:   ErrContextLookupFailed,
			wantContextID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:           "ownership verification fails with not found returns ErrContextNotFound",
			contextIDQuery: validContextID.String(),
			tenantID:       validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return ErrContextNotFound
			},
			expectedErr:   ErrContextNotFound,
			wantContextID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:           "success case returns contextID, tenantID, nil",
			contextIDQuery: validContextID.String(),
			tenantID:       validTenantID.String(),
			verifierFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				return nil
			},
			expectedErr:   nil,
			wantContextID: validContextID,
			wantTenantID:  validTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockVerifier := &MockContextOwnershipVerifier{
				VerifyOwnershipFunc: tt.verifierFunc,
			}

			var gotContextID, gotTenantID uuid.UUID

			var gotErr error

			handler := func(fiberCtx *fiber.Ctx) error {
				// Set tenant in user context
				ctx := setTenantInContext(tt.tenantID)
				fiberCtx.SetUserContext(ctx)

				gotContextID, gotTenantID, gotErr = ParseAndVerifyContextQuery(
					fiberCtx,
					mockVerifier,
					testTenantExtractor,
				)
				if gotErr != nil {
					return fiberCtx.Status(fiber.StatusBadRequest).
						JSON(fiber.Map{"error": gotErr.Error()})
				}

				return fiberCtx.JSON(fiber.Map{
					"contextId": gotContextID.String(),
					"tenantId":  gotTenantID.String(),
				})
			}

			app := setupFiberApp(t, handler, "/search")

			urlPath := "/search"
			if tt.contextIDQuery != "" {
				urlPath += "?contextId=" + tt.contextIDQuery
			}

			req := httptest.NewRequest(http.MethodGet, urlPath, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			// Check the error
			switch {
			case tt.expectedErr != nil:
				assert.ErrorIs(t, gotErr, tt.expectedErr)
			case tt.expectedErrMsg != "":
				require.Error(t, gotErr)
				assert.Contains(t, gotErr.Error(), tt.expectedErrMsg)
			default:
				assert.NoError(t, gotErr)
			}

			// Check returned values
			assert.Equal(t, tt.wantContextID, gotContextID)
			assert.Equal(t, tt.wantTenantID, gotTenantID)
		})
	}
}

func TestParseAndVerifyContextParam_VerifierPassesCorrectParameters(t *testing.T) {
	t.Parallel()

	expectedContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	expectedTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var receivedTenantID, receivedContextID uuid.UUID

	mockVerifier := &MockContextOwnershipVerifier{
		VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
			receivedTenantID = tenantID
			receivedContextID = contextID

			return nil
		},
	}

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(expectedTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, err := ParseAndVerifyContextParam(fiberCtx, mockVerifier, testTenantExtractor)
		if err != nil {
			return fiberCtx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/contexts/:contextId")

	req := httptest.NewRequest(http.MethodGet, "/contexts/"+expectedContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, expectedTenantID, receivedTenantID)
	assert.Equal(t, expectedContextID, receivedContextID)
}

func TestParseAndVerifyContextQuery_VerifierPassesCorrectParameters(t *testing.T) {
	t.Parallel()

	expectedContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	expectedTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var receivedTenantID, receivedContextID uuid.UUID

	mockVerifier := &MockContextOwnershipVerifier{
		VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
			receivedTenantID = tenantID
			receivedContextID = contextID

			return nil
		},
	}

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(expectedTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, err := ParseAndVerifyContextQuery(fiberCtx, mockVerifier, testTenantExtractor)
		if err != nil {
			return fiberCtx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/search")

	req := httptest.NewRequest(http.MethodGet, "/search?contextId="+expectedContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, expectedTenantID, receivedTenantID)
	assert.Equal(t, expectedContextID, receivedContextID)
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ErrMissingContextID has correct message",
			err:      ErrMissingContextID,
			expected: "context ID is required",
		},
		{
			name:     "ErrInvalidContextID has correct message",
			err:      ErrInvalidContextID,
			expected: "context ID must be a valid UUID",
		},
		{
			name:     "ErrTenantIDNotFound has correct message",
			err:      ErrTenantIDNotFound,
			expected: "tenant ID not found in request context",
		},
		{
			name:     "ErrContextNotOwned has correct message",
			err:      ErrContextNotOwned,
			expected: "context does not belong to the requesting tenant",
		},
		{
			name:     "ErrContextAccessDenied has correct message",
			err:      ErrContextAccessDenied,
			expected: "access to context denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestParseAndVerifyContextParam_WithDefaultTenant(t *testing.T) {
	t.Parallel()

	// When auth is disabled, the tenant extractor returns a default tenant ID.
	// This test verifies the function works correctly with the default tenant.

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	mockVerifier := &MockContextOwnershipVerifier{
		VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
			return nil
		},
	}

	handler := func(fiberCtx *fiber.Ctx) error {
		// Don't set tenant in context — extractor returns default
		_, _, err := ParseAndVerifyContextParam(fiberCtx, mockVerifier, testTenantExtractor)
		if err != nil {
			return fiberCtx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/contexts/:contextId")

	req := httptest.NewRequest(http.MethodGet, "/contexts/"+validContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	// The request should succeed because testTenantExtractor returns default tenant
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestParseAndVerifyContextQuery_WithDefaultTenant(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	mockVerifier := &MockContextOwnershipVerifier{
		VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
			return nil
		},
	}

	handler := func(fiberCtx *fiber.Ctx) error {
		// Don't set tenant in context — extractor returns default
		_, _, err := ParseAndVerifyContextQuery(fiberCtx, mockVerifier, testTenantExtractor)
		if err != nil {
			return fiberCtx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/search")

	req := httptest.NewRequest(http.MethodGet, "/search?contextId="+validContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestParseAndVerifyContextParam_UUIDEdgeCases(t *testing.T) {
	t.Parallel()

	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name      string
		contextID string
		wantErr   bool
	}{
		{
			name:      "valid UUID v4",
			contextID: "550e8400-e29b-41d4-a716-446655440000",
			wantErr:   false,
		},
		{
			name:      "valid UUID with uppercase",
			contextID: "550E8400-E29B-41D4-A716-446655440000",
			wantErr:   false,
		},
		{
			name:      "nil UUID",
			contextID: "00000000-0000-0000-0000-000000000000",
			wantErr:   false,
		},
		{
			name:      "UUID without hyphens is valid",
			contextID: "550e8400e29b41d4a716446655440000",
			wantErr:   false, // google/uuid.Parse accepts UUIDs without hyphens
		},
		{
			name:      "too short",
			contextID: "550e8400-e29b-41d4-a716",
			wantErr:   true,
		},
		{
			name:      "too long",
			contextID: "550e8400-e29b-41d4-a716-446655440000-extra",
			wantErr:   true,
		},
		{
			name:      "contains invalid characters",
			contextID: "550e8400-e29b-41d4-a716-44665544000g",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockVerifier := &MockContextOwnershipVerifier{
				VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
					return nil
				},
			}

			var gotErr error

			handler := func(fiberCtx *fiber.Ctx) error {
				ctx := setTenantInContext(validTenantID.String())
				fiberCtx.SetUserContext(ctx)

				_, _, gotErr = ParseAndVerifyContextParam(fiberCtx, mockVerifier, testTenantExtractor)
				if gotErr != nil {
					return fiberCtx.Status(fiber.StatusBadRequest).
						JSON(fiber.Map{"error": gotErr.Error()})
				}

				return fiberCtx.SendStatus(fiber.StatusOK)
			}

			app := setupFiberApp(t, handler, "/contexts/:contextId")

			req := httptest.NewRequest(http.MethodGet, "/contexts/"+tt.contextID, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			if tt.wantErr {
				require.Error(t, gotErr)
				assert.Contains(t, gotErr.Error(), "context ID must be a valid UUID")
			} else {
				assert.NoError(t, gotErr)
			}
		})
	}
}

func TestContextOwnershipVerifierInterface(t *testing.T) {
	t.Parallel()

	// Verify the interface is correctly defined
	var _ ContextOwnershipVerifier = (*MockContextOwnershipVerifier)(nil)
}

func TestParseAndVerifyContextParam_NilVerifier(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var gotErr error

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(validTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, gotErr = ParseAndVerifyContextParam(fiberCtx, nil, testTenantExtractor)
		if gotErr != nil {
			return fiberCtx.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": gotErr.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/contexts/:contextId")

	req := httptest.NewRequest(http.MethodGet, "/contexts/"+validContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Error(t, gotErr)
	assert.ErrorIs(t, gotErr, ErrContextAccessDenied)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestParseAndVerifyContextQuery_NilVerifier(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var gotErr error

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(validTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, gotErr = ParseAndVerifyContextQuery(fiberCtx, nil, testTenantExtractor)
		if gotErr != nil {
			return fiberCtx.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": gotErr.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/search")

	req := httptest.NewRequest(http.MethodGet, "/search?contextId="+validContextID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Error(t, gotErr)
	assert.ErrorIs(t, gotErr, ErrContextAccessDenied)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestParseAndVerifyContext_TenantIDAlwaysAvailable(t *testing.T) {
	t.Parallel()

	validContextID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	t.Run("tenant ID defaults when not explicitly set in context", func(t *testing.T) {
		t.Parallel()

		mockVerifier := &MockContextOwnershipVerifier{
			VerifyOwnershipFunc: func(ctx context.Context, tenantID, contextID uuid.UUID) error {
				// Verify tenant ID is the default, not empty or nil
				expectedDefaultTenantID := uuid.MustParse(defaultTestTenantID)
				assert.Equal(
					t,
					expectedDefaultTenantID,
					tenantID,
					"Expected default tenant ID when not explicitly set",
				)

				return nil
			},
		}

		handler := func(fiberCtx *fiber.Ctx) error {
			// Don't set any tenant in context - let extractor use default
			_, _, err := ParseAndVerifyContextParam(fiberCtx, mockVerifier, testTenantExtractor)
			if err != nil {
				return fiberCtx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			}

			return fiberCtx.SendStatus(fiber.StatusOK)
		}

		app := setupFiberApp(t, handler, "/contexts/:contextId")

		req := httptest.NewRequest(http.MethodGet, "/contexts/"+validContextID.String(), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer func() {
			_ = resp.Body.Close()
		}()

		// The request should succeed because testTenantExtractor returns default tenant
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run(
		"ErrTenantIDNotFound is unreachable with default tenant extractor",
		func(t *testing.T) {
			t.Parallel()

			// This test documents that ErrTenantIDNotFound cannot be triggered
			// when a tenant extractor that always returns a non-empty string is used.
			assert.NotNil(t, ErrTenantIDNotFound)
			assert.Contains(t, ErrTenantIDNotFound.Error(), "tenant ID not found")
		},
	)
}

func TestClassifyOwnershipError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputErr    error
		expectedErr error
	}{
		{
			name:        "ErrContextNotFound is preserved",
			inputErr:    ErrContextNotFound,
			expectedErr: ErrContextNotFound,
		},
		{
			name:        "ErrContextNotOwned is preserved",
			inputErr:    ErrContextNotOwned,
			expectedErr: ErrContextNotOwned,
		},
		{
			name:        "ErrContextNotActive is preserved",
			inputErr:    ErrContextNotActive,
			expectedErr: ErrContextNotActive,
		},
		{
			name:        "ErrContextAccessDenied is preserved",
			inputErr:    ErrContextAccessDenied,
			expectedErr: ErrContextAccessDenied,
		},
		{
			name:        "unknown error is wrapped with ErrContextLookupFailed",
			inputErr:    errors.New("pq: connection refused"),
			expectedErr: ErrContextLookupFailed,
		},
		{
			name:        "generic ownership error is wrapped with ErrContextLookupFailed",
			inputErr:    errTestOwnership,
			expectedErr: ErrContextLookupFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := classifyOwnershipError(tt.inputErr)
			assert.ErrorIs(t, result, tt.expectedErr)
		})
	}
}

func TestClassifyResourceOwnershipError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		label       string
		inputErr    error
		expectedErr error
		errContains string
	}{
		{
			name:        "ErrExceptionNotFound is preserved",
			label:       "exception",
			inputErr:    ErrExceptionNotFound,
			expectedErr: ErrExceptionNotFound,
		},
		{
			name:        "ErrExceptionAccessDenied is preserved",
			label:       "exception",
			inputErr:    ErrExceptionAccessDenied,
			expectedErr: ErrExceptionAccessDenied,
		},
		{
			name:        "ErrDisputeNotFound is preserved",
			label:       "dispute",
			inputErr:    ErrDisputeNotFound,
			expectedErr: ErrDisputeNotFound,
		},
		{
			name:        "ErrDisputeAccessDenied is preserved",
			label:       "dispute",
			inputErr:    ErrDisputeAccessDenied,
			expectedErr: ErrDisputeAccessDenied,
		},
		{
			name:        "unknown error is wrapped with ErrLookupFailed",
			label:       "exception",
			inputErr:    errors.New("pq: connection refused"),
			expectedErr: ErrLookupFailed,
			errContains: "exception",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := classifyResourceOwnershipError(tt.label, tt.inputErr)

			if tt.expectedErr != nil {
				assert.ErrorIs(t, result, tt.expectedErr)
			}

			if tt.errContains != "" {
				assert.Contains(t, result.Error(), tt.errContains)
			}
		})
	}
}

// mockSpan is a minimal mock implementation of trace.Span for testing.
type mockSpan struct {
	trace.Span
	attributes []attribute.KeyValue
}

func (m *mockSpan) SetAttributes(kv ...attribute.KeyValue) {
	m.attributes = append(m.attributes, kv...)
}

func TestSetHandlerSpanAttributes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	contextID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	t.Run("sets both tenant and context attributes", func(t *testing.T) {
		t.Parallel()

		span := &mockSpan{}
		SetHandlerSpanAttributes(span, tenantID, contextID)

		require.Len(t, span.attributes, 2)
		assert.Equal(t, "tenant.id", string(span.attributes[0].Key))
		assert.Equal(t, tenantID.String(), span.attributes[0].Value.AsString())
		assert.Equal(t, "context.id", string(span.attributes[1].Key))
		assert.Equal(t, contextID.String(), span.attributes[1].Value.AsString())
	})

	t.Run("sets only tenant when context is nil UUID", func(t *testing.T) {
		t.Parallel()

		span := &mockSpan{}
		SetHandlerSpanAttributes(span, tenantID, uuid.Nil)

		require.Len(t, span.attributes, 1)
		assert.Equal(t, "tenant.id", string(span.attributes[0].Key))
		assert.Equal(t, tenantID.String(), span.attributes[0].Value.AsString())
	})

	t.Run("handles nil span gracefully", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		SetHandlerSpanAttributes(nil, tenantID, contextID)
	})
}

func TestSetTenantSpanAttribute(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	t.Run("sets tenant attribute", func(t *testing.T) {
		t.Parallel()

		span := &mockSpan{}
		SetTenantSpanAttribute(span, tenantID)

		require.Len(t, span.attributes, 1)
		assert.Equal(t, "tenant.id", string(span.attributes[0].Key))
		assert.Equal(t, tenantID.String(), span.attributes[0].Value.AsString())
	})

	t.Run("handles nil span gracefully", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		SetTenantSpanAttribute(nil, tenantID)
	})
}

// MockExceptionOwnershipVerifier is a mock implementation of ExceptionOwnershipVerifier.
type MockExceptionOwnershipVerifier struct {
	VerifyOwnershipFunc func(ctx context.Context, exceptionID uuid.UUID) error
}

// VerifyOwnership implements ExceptionOwnershipVerifier.
func (m *MockExceptionOwnershipVerifier) VerifyOwnership(
	ctx context.Context,
	exceptionID uuid.UUID,
) error {
	if m.VerifyOwnershipFunc != nil {
		return m.VerifyOwnershipFunc(ctx, exceptionID)
	}

	return nil
}

// MockDisputeOwnershipVerifier is a mock implementation of DisputeOwnershipVerifier.
type MockDisputeOwnershipVerifier struct {
	VerifyOwnershipFunc func(ctx context.Context, disputeID uuid.UUID) error
}

// VerifyOwnership implements DisputeOwnershipVerifier.
func (m *MockDisputeOwnershipVerifier) VerifyOwnership(
	ctx context.Context,
	disputeID uuid.UUID,
) error {
	if m.VerifyOwnershipFunc != nil {
		return m.VerifyOwnershipFunc(ctx, disputeID)
	}

	return nil
}

func TestParseAndVerifyExceptionParam(t *testing.T) {
	t.Parallel()

	validExceptionID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name            string
		exceptionID     string
		tenantID        string
		verifierFunc    func(ctx context.Context, exceptionID uuid.UUID) error
		expectedErr     error
		expectedErrMsg  string
		wantExceptionID uuid.UUID
		wantTenantID    uuid.UUID
	}{
		{
			name:            "missing exceptionId parameter returns ErrMissingExceptionID",
			exceptionID:     "",
			tenantID:        validTenantID.String(),
			verifierFunc:    nil,
			expectedErr:     ErrMissingExceptionID,
			wantExceptionID: uuid.Nil,
			wantTenantID:    uuid.Nil,
		},
		{
			name:            "invalid UUID format returns ErrInvalidExceptionID",
			exceptionID:     "not-a-valid-uuid",
			tenantID:        validTenantID.String(),
			verifierFunc:    nil,
			expectedErrMsg:  "exception ID must be a valid UUID",
			wantExceptionID: uuid.Nil,
			wantTenantID:    uuid.Nil,
		},
		{
			name:        "ownership verification fails with known error returns sentinel",
			exceptionID: validExceptionID.String(),
			tenantID:    validTenantID.String(),
			verifierFunc: func(ctx context.Context, exceptionID uuid.UUID) error {
				return ErrExceptionNotFound
			},
			expectedErr:     ErrExceptionNotFound,
			wantExceptionID: uuid.Nil,
			wantTenantID:    uuid.Nil,
		},
		{
			name:        "success case returns exceptionID, tenantID, nil",
			exceptionID: validExceptionID.String(),
			tenantID:    validTenantID.String(),
			verifierFunc: func(ctx context.Context, exceptionID uuid.UUID) error {
				return nil
			},
			expectedErr:     nil,
			wantExceptionID: validExceptionID,
			wantTenantID:    validTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockVerifier := &MockExceptionOwnershipVerifier{
				VerifyOwnershipFunc: tt.verifierFunc,
			}

			var gotExceptionID, gotTenantID uuid.UUID

			var gotErr error

			handler := func(fiberCtx *fiber.Ctx) error {
				ctx := setTenantInContext(tt.tenantID)
				fiberCtx.SetUserContext(ctx)

				gotExceptionID, gotTenantID, gotErr = ParseAndVerifyExceptionParam(
					fiberCtx,
					mockVerifier,
					testTenantExtractor,
				)
				if gotErr != nil {
					return fiberCtx.Status(fiber.StatusBadRequest).
						JSON(fiber.Map{"error": gotErr.Error()})
				}

				return fiberCtx.JSON(fiber.Map{
					"exceptionId": gotExceptionID.String(),
					"tenantId":    gotTenantID.String(),
				})
			}

			pathPattern := "/exceptions/:exceptionId?"
			app := setupFiberApp(t, handler, pathPattern)

			urlPath := "/exceptions/"
			if tt.exceptionID != "" {
				urlPath += tt.exceptionID
			}

			req := httptest.NewRequest(http.MethodGet, urlPath, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			switch {
			case tt.expectedErr != nil:
				assert.ErrorIs(t, gotErr, tt.expectedErr)
			case tt.expectedErrMsg != "":
				require.Error(t, gotErr)
				assert.Contains(t, gotErr.Error(), tt.expectedErrMsg)
			default:
				assert.NoError(t, gotErr)
			}

			assert.Equal(t, tt.wantExceptionID, gotExceptionID)
			assert.Equal(t, tt.wantTenantID, gotTenantID)
		})
	}
}

func TestParseAndVerifyExceptionParam_NilVerifier(t *testing.T) {
	t.Parallel()

	validExceptionID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var gotErr error

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(validTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, gotErr = ParseAndVerifyExceptionParam(fiberCtx, nil, testTenantExtractor)
		if gotErr != nil {
			return fiberCtx.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": gotErr.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/exceptions/:exceptionId")

	req := httptest.NewRequest(http.MethodGet, "/exceptions/"+validExceptionID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Error(t, gotErr)
	assert.ErrorIs(t, gotErr, ErrExceptionAccessDenied)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestParseAndVerifyDisputeParam(t *testing.T) {
	t.Parallel()

	validDisputeID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name           string
		disputeID      string
		tenantID       string
		verifierFunc   func(ctx context.Context, disputeID uuid.UUID) error
		expectedErr    error
		expectedErrMsg string
		wantDisputeID  uuid.UUID
		wantTenantID   uuid.UUID
	}{
		{
			name:          "missing disputeId parameter returns ErrMissingDisputeID",
			disputeID:     "",
			tenantID:      validTenantID.String(),
			verifierFunc:  nil,
			expectedErr:   ErrMissingDisputeID,
			wantDisputeID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:           "invalid UUID format returns ErrInvalidDisputeID",
			disputeID:      "not-a-valid-uuid",
			tenantID:       validTenantID.String(),
			verifierFunc:   nil,
			expectedErrMsg: "dispute ID must be a valid UUID",
			wantDisputeID:  uuid.Nil,
			wantTenantID:   uuid.Nil,
		},
		{
			name:      "ownership verification fails with known error returns sentinel",
			disputeID: validDisputeID.String(),
			tenantID:  validTenantID.String(),
			verifierFunc: func(ctx context.Context, disputeID uuid.UUID) error {
				return ErrDisputeNotFound
			},
			expectedErr:   ErrDisputeNotFound,
			wantDisputeID: uuid.Nil,
			wantTenantID:  uuid.Nil,
		},
		{
			name:      "success case returns disputeID, tenantID, nil",
			disputeID: validDisputeID.String(),
			tenantID:  validTenantID.String(),
			verifierFunc: func(ctx context.Context, disputeID uuid.UUID) error {
				return nil
			},
			expectedErr:   nil,
			wantDisputeID: validDisputeID,
			wantTenantID:  validTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockVerifier := &MockDisputeOwnershipVerifier{
				VerifyOwnershipFunc: tt.verifierFunc,
			}

			var gotDisputeID, gotTenantID uuid.UUID

			var gotErr error

			handler := func(fiberCtx *fiber.Ctx) error {
				ctx := setTenantInContext(tt.tenantID)
				fiberCtx.SetUserContext(ctx)

				gotDisputeID, gotTenantID, gotErr = ParseAndVerifyDisputeParam(
					fiberCtx,
					mockVerifier,
					testTenantExtractor,
				)
				if gotErr != nil {
					return fiberCtx.Status(fiber.StatusBadRequest).
						JSON(fiber.Map{"error": gotErr.Error()})
				}

				return fiberCtx.JSON(fiber.Map{
					"disputeId": gotDisputeID.String(),
					"tenantId":  gotTenantID.String(),
				})
			}

			pathPattern := "/disputes/:disputeId?"
			app := setupFiberApp(t, handler, pathPattern)

			urlPath := "/disputes/"
			if tt.disputeID != "" {
				urlPath += tt.disputeID
			}

			req := httptest.NewRequest(http.MethodGet, urlPath, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer func() {
				_ = resp.Body.Close()
			}()

			switch {
			case tt.expectedErr != nil:
				assert.ErrorIs(t, gotErr, tt.expectedErr)
			case tt.expectedErrMsg != "":
				require.Error(t, gotErr)
				assert.Contains(t, gotErr.Error(), tt.expectedErrMsg)
			default:
				assert.NoError(t, gotErr)
			}

			assert.Equal(t, tt.wantDisputeID, gotDisputeID)
			assert.Equal(t, tt.wantTenantID, gotTenantID)
		})
	}
}

func TestParseAndVerifyDisputeParam_NilVerifier(t *testing.T) {
	t.Parallel()

	validDisputeID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	validTenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	var gotErr error

	handler := func(fiberCtx *fiber.Ctx) error {
		ctx := setTenantInContext(validTenantID.String())
		fiberCtx.SetUserContext(ctx)

		_, _, gotErr = ParseAndVerifyDisputeParam(fiberCtx, nil, testTenantExtractor)
		if gotErr != nil {
			return fiberCtx.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": gotErr.Error()})
		}

		return fiberCtx.SendStatus(fiber.StatusOK)
	}

	app := setupFiberApp(t, handler, "/disputes/:disputeId")

	req := httptest.NewRequest(http.MethodGet, "/disputes/"+validDisputeID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Error(t, gotErr)
	assert.ErrorIs(t, gotErr, ErrDisputeAccessDenied)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSetExceptionSpanAttributes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	exceptionID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	t.Run("sets both tenant and exception attributes", func(t *testing.T) {
		t.Parallel()

		span := &mockSpan{}
		SetExceptionSpanAttributes(span, tenantID, exceptionID)

		require.Len(t, span.attributes, 2)
		assert.Equal(t, "tenant.id", string(span.attributes[0].Key))
		assert.Equal(t, tenantID.String(), span.attributes[0].Value.AsString())
		assert.Equal(t, "exception.id", string(span.attributes[1].Key))
		assert.Equal(t, exceptionID.String(), span.attributes[1].Value.AsString())
	})

	t.Run("handles nil span gracefully", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		SetExceptionSpanAttributes(nil, tenantID, exceptionID)
	})
}

func TestSetDisputeSpanAttributes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	disputeID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440001")

	t.Run("sets both tenant and dispute attributes", func(t *testing.T) {
		t.Parallel()

		span := &mockSpan{}
		SetDisputeSpanAttributes(span, tenantID, disputeID)

		require.Len(t, span.attributes, 2)
		assert.Equal(t, "tenant.id", string(span.attributes[0].Key))
		assert.Equal(t, tenantID.String(), span.attributes[0].Value.AsString())
		assert.Equal(t, "dispute.id", string(span.attributes[1].Key))
		assert.Equal(t, disputeID.String(), span.attributes[1].Value.AsString())
	})

	t.Run("handles nil span gracefully", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		SetDisputeSpanAttributes(nil, tenantID, disputeID)
	})
}

func TestExceptionSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ErrMissingExceptionID",
			err:      ErrMissingExceptionID,
			expected: "exception ID is required",
		},
		{
			name:     "ErrInvalidExceptionID",
			err:      ErrInvalidExceptionID,
			expected: "exception ID must be a valid UUID",
		},
		{
			name:     "ErrExceptionNotFound",
			err:      ErrExceptionNotFound,
			expected: "exception not found",
		},
		{
			name:     "ErrExceptionAccessDenied",
			err:      ErrExceptionAccessDenied,
			expected: "access to exception denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestDisputeSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ErrMissingDisputeID",
			err:      ErrMissingDisputeID,
			expected: "dispute ID is required",
		},
		{
			name:     "ErrInvalidDisputeID",
			err:      ErrInvalidDisputeID,
			expected: "dispute ID must be a valid UUID",
		},
		{
			name:     "ErrDisputeNotFound",
			err:      ErrDisputeNotFound,
			expected: "dispute not found",
		},
		{
			name:     "ErrDisputeAccessDenied",
			err:      ErrDisputeAccessDenied,
			expected: "access to dispute denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestExceptionOwnershipVerifierInterface(t *testing.T) {
	t.Parallel()

	var _ ExceptionOwnershipVerifier = (*MockExceptionOwnershipVerifier)(nil)
}

func TestDisputeOwnershipVerifierInterface(t *testing.T) {
	t.Parallel()

	var _ DisputeOwnershipVerifier = (*MockDisputeOwnershipVerifier)(nil)
}

func TestTenantExtractorType(t *testing.T) {
	t.Parallel()

	// Verify TenantExtractor is a function type that can be used
	var extractor TenantExtractor = func(ctx context.Context) string {
		return "test-tenant-id"
	}

	result := extractor(context.Background())
	assert.Equal(t, "test-tenant-id", result)
}
