//go:build unit

package outbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextWithTenantID_HandlesNilContextAndWhitespace(t *testing.T) {
	t.Parallel()

	ctx := ContextWithTenantID(nil, "  tenant-1  ")
	tenantID, ok := TenantIDFromContext(ctx)

	require.True(t, ok)
	require.Equal(t, "tenant-1", tenantID)
}

func TestTenantIDFromContext_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := ContextWithTenantID(context.Background(), "tenant-42")
	tenantID, ok := TenantIDFromContext(ctx)

	require.True(t, ok)
	require.Equal(t, "tenant-42", tenantID)
}

func TestTenantIDFromContext_InvalidCases(t *testing.T) {
	t.Parallel()

	tenantID, ok := TenantIDFromContext(nil)
	require.False(t, ok)
	require.Empty(t, tenantID)

	ctx := ContextWithTenantID(context.Background(), "   ")
	tenantID, ok = TenantIDFromContext(ctx)
	require.False(t, ok)
	require.Empty(t, tenantID)
}
