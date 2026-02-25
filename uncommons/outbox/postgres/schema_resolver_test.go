//go:build unit

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/outbox"
	libPostgres "github.com/LerianStudio/lib-uncommons/v2/uncommons/postgres"
	"github.com/stretchr/testify/require"
)

func TestNewSchemaResolver_NilClient(t *testing.T) {
	t.Parallel()

	resolver, err := NewSchemaResolver(nil)
	require.Nil(t, resolver)
	require.ErrorIs(t, err, ErrConnectionRequired)
}

func TestSchemaResolver_ApplyTenantValidation(t *testing.T) {
	t.Parallel()

	resolver := &SchemaResolver{}

	require.ErrorIs(t, resolver.ApplyTenant(context.Background(), nil, "tenant"), ErrTransactionRequired)
}

func TestSchemaResolver_ApplyTenantNilReceiver(t *testing.T) {
	t.Parallel()

	var resolver *SchemaResolver

	err := resolver.ApplyTenant(context.Background(), &sql.Tx{}, "tenant")
	require.ErrorIs(t, err, ErrConnectionRequired)
}

func TestSchemaResolver_ApplyTenantNoopPaths(t *testing.T) {
	t.Parallel()

	resolver, err := NewSchemaResolver(
		&libPostgres.Client{},
		WithDefaultTenantID("tenant-default"),
		WithAllowEmptyTenant(),
	)
	require.NoError(t, err)

	tx := &sql.Tx{}

	require.NoError(t, resolver.ApplyTenant(context.Background(), tx, ""))
	require.NoError(t, resolver.ApplyTenant(context.Background(), tx, "tenant-default"))
}

func TestNewSchemaResolver_DefaultRequiresTenant(t *testing.T) {
	t.Parallel()

	resolver, err := NewSchemaResolver(&libPostgres.Client{})
	require.NoError(t, err)

	require.True(t, resolver.RequiresTenant())
}

func TestNewSchemaResolver_WithAllowEmptyTenantDisablesRequirement(t *testing.T) {
	t.Parallel()

	resolver, err := NewSchemaResolver(&libPostgres.Client{}, WithAllowEmptyTenant())
	require.NoError(t, err)

	require.False(t, resolver.RequiresTenant())
}

func TestNewSchemaResolver_DefaultTenantValidationInStrictMode(t *testing.T) {
	t.Parallel()

	resolver, err := NewSchemaResolver(&libPostgres.Client{}, WithDefaultTenantID("default-tenant"))
	require.Nil(t, resolver)
	require.ErrorIs(t, err, ErrDefaultTenantIDInvalid)

	resolver, err = NewSchemaResolver(
		&libPostgres.Client{},
		WithAllowEmptyTenant(),
		WithDefaultTenantID("default-tenant"),
	)
	require.NoError(t, err)
	require.NotNil(t, resolver)
}

func TestSchemaResolver_ApplyTenantRequireTenant(t *testing.T) {
	t.Parallel()

	resolver := &SchemaResolver{requireTenant: true}

	err := resolver.ApplyTenant(context.Background(), &sql.Tx{}, "")
	require.ErrorIs(t, err, outbox.ErrTenantIDRequired)
}

func TestSchemaResolver_ApplyTenantRejectsInvalidTenantID(t *testing.T) {
	t.Parallel()

	resolver := &SchemaResolver{}
	err := resolver.ApplyTenant(context.Background(), &sql.Tx{}, "tenant-invalid")
	require.ErrorContains(t, err, "invalid tenant id format")
}

func TestSchemaResolver_DiscoverTenantsNilReceiver(t *testing.T) {
	t.Parallel()

	var resolver *SchemaResolver

	tenants, err := resolver.DiscoverTenants(context.Background())
	require.Nil(t, tenants)
	require.ErrorIs(t, err, ErrConnectionRequired)
}
