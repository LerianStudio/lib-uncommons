package valkey

import (
	"context"
	"testing"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/core"
	"github.com/stretchr/testify/assert"
)

func TestGetKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tenantID string
		key      string
		expected string
	}{
		{name: "prefixes key with tenant", tenantID: "tenant-123", key: "orders", expected: "tenant:tenant-123:orders"},
		{name: "returns key unchanged when tenant empty", tenantID: "", key: "orders", expected: "orders"},
		{name: "handles empty key", tenantID: "tenant-123", key: "", expected: "tenant:tenant-123:"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, GetKey(tt.tenantID, tt.key))
		})
	}
}

func TestGetKeyFromContext(t *testing.T) {
	t.Parallel()

	ctx := core.SetTenantIDInContext(context.Background(), "tenant-ctx")
	assert.Equal(t, "tenant:tenant-ctx:orders", GetKeyFromContext(ctx, "orders"))
	assert.Equal(t, "orders", GetKeyFromContext(context.Background(), "orders"))
	assert.Equal(t, "orders", GetKeyFromContext(nil, "orders"))
}

func TestGetPattern(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "tenant:tenant-123:orders:*", GetPattern("tenant-123", "orders:*"))
	assert.Equal(t, "orders:*", GetPattern("", "orders:*"))
}

func TestGetPatternFromContext(t *testing.T) {
	t.Parallel()

	ctx := core.SetTenantIDInContext(context.Background(), "tenant-ctx")
	assert.Equal(t, "tenant:tenant-ctx:orders:*", GetPatternFromContext(ctx, "orders:*"))
	assert.Equal(t, "orders:*", GetPatternFromContext(context.Background(), "orders:*"))
	assert.Equal(t, "orders:*", GetPatternFromContext(nil, "orders:*"))
}

func TestStripTenantPrefix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "orders:1", StripTenantPrefix("tenant-123", "tenant:tenant-123:orders:1"))
	assert.Equal(t, "orders:1", StripTenantPrefix("", "orders:1"))
	assert.Equal(t, "tenant:other:orders:1", StripTenantPrefix("tenant-123", "tenant:other:orders:1"))
}
