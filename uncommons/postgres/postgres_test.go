//go:build unit

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/bxcodec/dbresolver/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResolver struct {
	pingErr   error
	closeErr  error
	pingCtx   context.Context
	closeCall atomic.Int32
}

func (f *fakeResolver) Begin() (dbresolver.Tx, error) { return nil, nil }

func (f *fakeResolver) BeginTx(context.Context, *sql.TxOptions) (dbresolver.Tx, error) {
	return nil, nil
}

func (f *fakeResolver) Close() error {
	f.closeCall.Add(1)

	return f.closeErr
}

func (f *fakeResolver) Conn(context.Context) (dbresolver.Conn, error) { return nil, nil }

func (f *fakeResolver) Driver() driver.Driver { return nil }

func (f *fakeResolver) Exec(string, ...interface{}) (sql.Result, error) { return nil, nil }

func (f *fakeResolver) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, nil
}

func (f *fakeResolver) Ping() error { return nil }

func (f *fakeResolver) PingContext(ctx context.Context) error {
	f.pingCtx = ctx

	return f.pingErr
}

func (f *fakeResolver) Prepare(string) (dbresolver.Stmt, error) { return nil, nil }

func (f *fakeResolver) PrepareContext(context.Context, string) (dbresolver.Stmt, error) {
	return nil, nil
}

func (f *fakeResolver) Query(string, ...interface{}) (*sql.Rows, error) { return nil, nil }

func (f *fakeResolver) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, nil
}

func (f *fakeResolver) QueryRow(string, ...interface{}) *sql.Row { return &sql.Row{} }

func (f *fakeResolver) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

func (f *fakeResolver) SetConnMaxIdleTime(time.Duration) {}

func (f *fakeResolver) SetConnMaxLifetime(time.Duration) {}

func (f *fakeResolver) SetMaxIdleConns(int) {}

func (f *fakeResolver) SetMaxOpenConns(int) {}

func (f *fakeResolver) PrimaryDBs() []*sql.DB { return nil }

func (f *fakeResolver) ReplicaDBs() []*sql.DB { return nil }

func (f *fakeResolver) Stats() sql.DBStats { return sql.DBStats{} }

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("skipping: cannot open postgres connection (set POSTGRES_DSN to configure): %v", err)
	}

	return db
}

// withPatchedDependencies replaces package-level dependency functions for testing.
// WARNING: Tests using this helper must NOT call t.Parallel() as it mutates global state.
func withPatchedDependencies(t *testing.T, openFn func(string, string) (*sql.DB, error), resolverFn func(*sql.DB, *sql.DB) (dbresolver.DB, error), migrateFn func(*sql.DB, string, string, bool, log.Logger) error) {
	t.Helper()

	originalOpen := dbOpenFn
	originalResolver := createResolverFn
	originalMigrations := runMigrationsFn

	dbOpenFn = openFn
	createResolverFn = resolverFn
	runMigrationsFn = migrateFn

	t.Cleanup(func() {
		dbOpenFn = originalOpen
		createResolverFn = originalResolver
		runMigrationsFn = originalMigrations
	})
}

func validConfig() Config {
	return Config{
		PrimaryDSN: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		ReplicaDSN: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
	}
}

func TestNewConfigValidationAndDefaults(t *testing.T) {
	t.Run("rejects missing dsn", func(t *testing.T) {
		_, err := New(Config{})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("applies defaults", func(t *testing.T) {
		client, err := New(validConfig())

		require.NoError(t, err)
		require.NotNil(t, client)
		assert.NotNil(t, client.cfg.Logger)
		assert.Equal(t, defaultMaxOpenConns, client.cfg.MaxOpenConnections)
		assert.Equal(t, defaultMaxIdleConns, client.cfg.MaxIdleConnections)
	})
}

func TestConnectRequiresContext(t *testing.T) {
	client, err := New(validConfig())
	require.NoError(t, err)

	err = client.Connect(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilContext)
}

func TestDBRequiresContext(t *testing.T) {
	client, err := New(validConfig())
	require.NoError(t, err)

	_, err = client.Resolver(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilContext)
}

func TestConnectSanitizesSensitiveError(t *testing.T) {
	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) {
			return nil, errors.New("parse postgres://alice:supersecret@db.internal:5432/main failed password=supersecret")
		},
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return nil, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	client, err := New(validConfig())
	require.NoError(t, err)

	err = client.Connect(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.Contains(t, err.Error(), "://***@")
	assert.Contains(t, err.Error(), "password=***")
}

func TestConnectAtomicSwapKeepsOldOnFailure(t *testing.T) {
	oldResolver := &fakeResolver{}
	newResolver := &fakeResolver{pingErr: errors.New("boom")}

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return newResolver, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	client, err := New(validConfig())
	require.NoError(t, err)
	client.resolver = oldResolver

	err = client.Connect(context.Background())
	require.Error(t, err)
	assert.Equal(t, oldResolver, client.resolver)
	assert.Equal(t, int32(0), oldResolver.closeCall.Load())
	assert.Equal(t, int32(1), newResolver.closeCall.Load())
}

func TestConnectAtomicSwapClosesPreviousOnSuccess(t *testing.T) {
	oldResolver := &fakeResolver{}
	newResolver := &fakeResolver{}

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return newResolver, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	client, err := New(validConfig())
	require.NoError(t, err)
	client.resolver = oldResolver

	err = client.Connect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), oldResolver.closeCall.Load())
	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.True(t, connected)

	assert.NoError(t, client.Close())
}

func TestDBLazyConnect(t *testing.T) {
	resolver := &fakeResolver{}

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return resolver, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	client, err := New(validConfig())
	require.NoError(t, err)

	db, err := client.Resolver(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, db)
	assert.NotNil(t, resolver.pingCtx)

	assert.NoError(t, client.Close())
}

func TestCloseIsIdempotent(t *testing.T) {
	resolver := &fakeResolver{}

	client, err := New(validConfig())
	require.NoError(t, err)
	client.resolver = resolver

	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.False(t, connected)
	assert.Equal(t, int32(1), resolver.closeCall.Load())
}

func TestNewMigratorValidation(t *testing.T) {
	t.Run("requires db name", func(t *testing.T) {
		_, err := NewMigrator(MigrationConfig{PrimaryDSN: "postgres://localhost:5432/postgres"})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidDatabaseName)
	})

	t.Run("requires component or path", func(t *testing.T) {
		_, err := NewMigrator(MigrationConfig{PrimaryDSN: "postgres://localhost:5432/postgres", DatabaseName: "ledger"})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func TestMigratorUpRunsExplicitly(t *testing.T) {
	var migrationCalls atomic.Int32

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return &fakeResolver{}, nil },
		func(*sql.DB, string, string, bool, log.Logger) error {
			migrationCalls.Add(1)
			return nil
		},
	)

	migrator, err := NewMigrator(MigrationConfig{
		PrimaryDSN:     "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		DatabaseName:   "postgres",
		MigrationsPath: "components/ledger/migrations",
	})
	require.NoError(t, err)

	err = migrator.Up(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), migrationCalls.Load())
}
