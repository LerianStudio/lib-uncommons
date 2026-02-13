package mongo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func withDeps(deps clientDeps) Option {
	return func(current *clientDeps) {
		*current = deps
	}
}

func baseConfig() Config {
	return Config{
		URI:      "mongodb://localhost:27017",
		Database: "app",
	}
}

func TestNewClient_ValidatesInput(t *testing.T) {
	t.Parallel()

	t.Run("nil_context", func(t *testing.T) {
		t.Parallel()

		client, err := NewClient(nil, baseConfig())
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrNilContext)
	})

	t.Run("empty_uri", func(t *testing.T) {
		t.Parallel()

		cfg := baseConfig()
		cfg.URI = ""

		client, err := NewClient(context.Background(), cfg)
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrEmptyURI)
	})

	t.Run("empty_database", func(t *testing.T) {
		t.Parallel()

		cfg := baseConfig()
		cfg.Database = "  "

		client, err := NewClient(context.Background(), cfg)
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrEmptyDatabaseName)
	})
}

func TestNewClient_ConnectAndPingFailures(t *testing.T) {
	t.Parallel()

	t.Run("connect_failure", func(t *testing.T) {
		t.Parallel()

		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return nil, errors.New("dial failed")
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrConnect)
	})

	t.Run("nil_client_returned", func(t *testing.T) {
		t.Parallel()

		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return nil, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrNilMongoClient)
	})

	t.Run("ping_failure_disconnects", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		var disconnectCalls atomic.Int32

		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error {
				return errors.New("ping failed")
			},
			disconnect: func(context.Context, *mongo.Client) error {
				disconnectCalls.Add(1)
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		assert.Nil(t, client)
		assert.ErrorIs(t, err, ErrPing)
		assert.EqualValues(t, 1, disconnectCalls.Load())
	})
}

func TestClient_ConnectIsIdempotent(t *testing.T) {
	t.Parallel()

	fakeClient := &mongo.Client{}
	var connectCalls atomic.Int32

	deps := clientDeps{
		connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
			connectCalls.Add(1)
			return fakeClient, nil
		},
		ping: func(context.Context, *mongo.Client) error { return nil },
		disconnect: func(context.Context, *mongo.Client) error {
			return nil
		},
		createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
			return nil
		},
	}

	client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
	require.NoError(t, err)

	assert.NoError(t, client.Connect(context.Background()))
	assert.EqualValues(t, 1, connectCalls.Load())
}

func TestClient_ClientAndDatabase(t *testing.T) {
	t.Parallel()

	fakeClient := &mongo.Client{}
	deps := clientDeps{
		connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
			return fakeClient, nil
		},
		ping: func(context.Context, *mongo.Client) error { return nil },
		disconnect: func(context.Context, *mongo.Client) error {
			return nil
		},
		createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
			return nil
		},
	}

	client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
	require.NoError(t, err)

	t.Run("nil_context", func(t *testing.T) {
		t.Parallel()

		mongoClient, callErr := client.Client(nil)
		assert.Nil(t, mongoClient)
		assert.ErrorIs(t, callErr, ErrNilContext)
	})

	t.Run("database_name", func(t *testing.T) {
		t.Parallel()

		databaseName, err := client.DatabaseName()
		require.NoError(t, err)
		assert.Equal(t, "app", databaseName)
	})

	t.Run("database_returns_handle", func(t *testing.T) {
		t.Parallel()

		db, callErr := client.Database(context.Background())
		require.NoError(t, callErr)
		assert.Equal(t, "app", db.Name())
	})
}

func TestClient_Close(t *testing.T) {
	t.Parallel()

	t.Run("nil_receiver", func(t *testing.T) {
		t.Parallel()

		var client *Client
		assert.ErrorIs(t, client.Close(context.Background()), ErrClientClosed)
	})

	t.Run("nil_context", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		assert.ErrorIs(t, client.Close(nil), ErrNilContext)
	})

	t.Run("disconnect_failure_clears_client", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return errors.New("disconnect failed")
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		err = client.Close(context.Background())
		assert.ErrorIs(t, err, ErrDisconnect)

		mongoClient, callErr := client.Client(context.Background())
		assert.Nil(t, mongoClient)
		assert.ErrorIs(t, callErr, ErrClientClosed)
	})

	t.Run("close_is_idempotent", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		var disconnectCalls atomic.Int32
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				disconnectCalls.Add(1)
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		require.NoError(t, client.Close(context.Background()))
		require.NoError(t, client.Close(context.Background()))
		assert.EqualValues(t, 1, disconnectCalls.Load())
	})
}

func TestClient_EnsureIndexes(t *testing.T) {
	t.Parallel()

	t.Run("validates_arguments", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		err = client.EnsureIndexes(nil, "users", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}})
		assert.ErrorIs(t, err, ErrNilContext)

		err = client.EnsureIndexes(context.Background(), " ", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}})
		assert.ErrorIs(t, err, ErrEmptyCollectionName)

		err = client.EnsureIndexes(context.Background(), "users")
		assert.ErrorIs(t, err, ErrEmptyIndexes)
	})

	t.Run("creates_all_indexes", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		var createCalls atomic.Int32
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(_ context.Context, client *mongo.Client, database, collection string, index mongo.IndexModel) error {
				createCalls.Add(1)
				assert.Same(t, fakeClient, client)
				assert.Equal(t, "app", database)
				assert.Equal(t, "users", collection)
				assert.NotNil(t, index.Keys)
				return nil
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		err = client.EnsureIndexes(
			context.Background(),
			"users",
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "created_at", Value: -1}}},
		)
		require.NoError(t, err)
		assert.EqualValues(t, 2, createCalls.Load())
	})

	t.Run("wraps_create_index_error", func(t *testing.T) {
		t.Parallel()

		fakeClient := &mongo.Client{}
		deps := clientDeps{
			connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
				return fakeClient, nil
			},
			ping: func(context.Context, *mongo.Client) error { return nil },
			disconnect: func(context.Context, *mongo.Client) error {
				return nil
			},
			createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
				return errors.New("duplicate options")
			},
		}

		client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
		require.NoError(t, err)

		err = client.EnsureIndexes(context.Background(), "users", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}})
		assert.ErrorIs(t, err, ErrCreateIndex)
	})
}

func TestClient_ConcurrentClientReads(t *testing.T) {
	t.Parallel()

	fakeClient := &mongo.Client{}
	deps := clientDeps{
		connect: func(context.Context, *options.ClientOptions) (*mongo.Client, error) {
			return fakeClient, nil
		},
		ping: func(context.Context, *mongo.Client) error { return nil },
		disconnect: func(context.Context, *mongo.Client) error {
			return nil
		},
		createIndex: func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error {
			return nil
		},
	}

	client, err := NewClient(context.Background(), baseConfig(), withDeps(deps))
	require.NoError(t, err)

	const workers = 50
	results := make([]*mongo.Client, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = client.Client(context.Background())
		}(i)
	}

	wg.Wait()

	for i := 0; i < workers; i++ {
		assert.NoError(t, errs[i])
		assert.Same(t, fakeClient, results[i])
	}
}

func TestIndexKeysString(t *testing.T) {
	t.Parallel()

	t.Run("bson_d_preserves_order", func(t *testing.T) {
		t.Parallel()

		keys := bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}
		assert.Equal(t, "tenant_id,created_at", indexKeysString(keys))
	})

	t.Run("bson_m_is_sorted", func(t *testing.T) {
		t.Parallel()

		keys := bson.M{"zeta": 1, "alpha": 1, "middle": -1}
		assert.Equal(t, "alpha,middle,zeta", indexKeysString(keys))
	})

	t.Run("unknown_type", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "<unknown>", indexKeysString(42))
	})
}
