package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
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

	db, err := sql.Open("pgx", "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable")
	require.NoError(t, err)

	return db
}

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

func TestInitDefaults(t *testing.T) {
	t.Run("sets logger and defaults for non-positive values", func(t *testing.T) {
		pc := &PostgresConnection{
			MaxOpenConnections: -1,
			MaxIdleConnections: 0,
		}

		pc.initDefaults()

		assert.NotNil(t, pc.Logger)
		assert.Equal(t, defaultMaxOpenConns, pc.MaxOpenConnections)
		assert.Equal(t, defaultMaxIdleConns, pc.MaxIdleConnections)
	})

	t.Run("preserves explicit values", func(t *testing.T) {
		logger := &log.NoneLogger{}
		pc := &PostgresConnection{
			Logger:             logger,
			MaxOpenConnections: 77,
			MaxIdleConnections: 22,
		}

		pc.initDefaults()

		assert.Equal(t, logger, pc.Logger)
		assert.Equal(t, 77, pc.MaxOpenConnections)
		assert.Equal(t, 22, pc.MaxIdleConnections)
	})
}

func TestGetMigrationsPath(t *testing.T) {
	t.Run("sanitizes explicit path and rejects traversal", func(t *testing.T) {
		pc := &PostgresConnection{MigrationsPath: "../../etc/passwd", Logger: &log.NoneLogger{}}

		_, err := pc.getMigrationsPath()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid migrations path")
	})

	t.Run("sanitizes component", func(t *testing.T) {
		pc := &PostgresConnection{Component: "../../ledger", Logger: &log.NoneLogger{}}

		path, err := pc.getMigrationsPath()

		require.NoError(t, err)
		assert.NotContains(t, path, "..")
		assert.Contains(t, path, "components")
		assert.Contains(t, path, "ledger")
	})

	t.Run("rejects empty component", func(t *testing.T) {
		pc := &PostgresConnection{Component: "", Logger: &log.NoneLogger{}}

		_, err := pc.getMigrationsPath()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid component name")
	})
}

func TestConnectUsesBackgroundContextWhenNil(t *testing.T) {
	resolver := &fakeResolver{}

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return resolver, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	pc := &PostgresConnection{
		ConnectionStringPrimary: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		ConnectionStringReplica: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		PrimaryDBName:           "postgres",
		Component:               "ledger",
		MigrationsPath:          "components/ledger/migrations",
	}

	err := pc.Connect(nil)
	require.NoError(t, err)

	assert.True(t, pc.IsConnected())
	assert.NotNil(t, resolver.pingCtx)

	assert.NoError(t, pc.Close())
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

	pc := &PostgresConnection{Logger: &log.NoneLogger{}}

	err := pc.Connect(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.Contains(t, err.Error(), "://***@")
	assert.Contains(t, err.Error(), "password=***")
}

func TestConnectClosesPreviousResolverOnReconnect(t *testing.T) {
	previous := &fakeResolver{}
	next := &fakeResolver{}

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) { return testDB(t), nil },
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return next, nil },
		func(*sql.DB, string, string, bool, log.Logger) error { return nil },
	)

	pc := &PostgresConnection{
		ConnectionStringPrimary: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		ConnectionStringReplica: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		PrimaryDBName:           "postgres",
		MigrationsPath:          "components/ledger/migrations",
		connectionDB:            previous,
		connected:               true,
	}

	err := pc.Connect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), previous.closeCall.Load())

	assert.NoError(t, pc.Close())
}

func TestConnectFailsWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pc := &PostgresConnection{}

	err := pc.Connect(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestGetDBConcurrentSingleInitialization(t *testing.T) {
	resolver := &fakeResolver{}
	var migrationsCalls atomic.Int32
	var openCalls atomic.Int32

	withPatchedDependencies(
		t,
		func(string, string) (*sql.DB, error) {
			openCalls.Add(1)
			return testDB(t), nil
		},
		func(*sql.DB, *sql.DB) (dbresolver.DB, error) { return resolver, nil },
		func(*sql.DB, string, string, bool, log.Logger) error {
			migrationsCalls.Add(1)
			return nil
		},
	)

	pc := &PostgresConnection{
		ConnectionStringPrimary: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		ConnectionStringReplica: "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable",
		PrimaryDBName:           "postgres",
		MigrationsPath:          "components/ledger/migrations",
	}

	const workers = 50
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := pc.GetDB()
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), migrationsCalls.Load())
	assert.Equal(t, int32(2), openCalls.Load())

	assert.NoError(t, pc.Close())
}

func TestCloseIsIdempotent(t *testing.T) {
	resolver := &fakeResolver{}

	pc := &PostgresConnection{connectionDB: resolver, connected: true}

	require.NoError(t, pc.Close())
	require.NoError(t, pc.Close())
	assert.False(t, pc.IsConnected())
	assert.Equal(t, int32(1), resolver.closeCall.Load())
}

func TestValidateDBName(t *testing.T) {
	require.NoError(t, validateDBName("ledger_db"))
	require.Error(t, validateDBName(""))
	require.Error(t, validateDBName("bad-name"))
	require.Error(t, validateDBName("1ledger"))
}
