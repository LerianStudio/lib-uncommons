package mongo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/assert"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	defaultServerSelectionTimeout = 5 * time.Second
	defaultHeartbeatInterval      = 10 * time.Second
)

var (
	ErrNilContext          = errors.New("context cannot be nil")
	ErrEmptyURI            = errors.New("mongo uri cannot be empty")
	ErrEmptyDatabaseName   = errors.New("database name cannot be empty")
	ErrClientClosed        = errors.New("mongo client is closed")
	ErrEmptyCollectionName = errors.New("collection name cannot be empty")
	ErrEmptyIndexes        = errors.New("at least one index must be provided")
	ErrConnect             = errors.New("mongo connect failed")
	ErrPing                = errors.New("mongo ping failed")
	ErrDisconnect          = errors.New("mongo disconnect failed")
	ErrCreateIndex         = errors.New("mongo create index failed")
	ErrNilMongoClient      = errors.New("mongo driver returned nil client")
)

// Config defines MongoDB connection and pool behavior.
type Config struct {
	URI                    string
	Database               string
	MaxPoolSize            uint64
	ServerSelectionTimeout time.Duration
	HeartbeatInterval      time.Duration
	Logger                 log.Logger
}

func (cfg Config) validate() error {
	if strings.TrimSpace(cfg.URI) == "" {
		return ErrEmptyURI
	}

	if strings.TrimSpace(cfg.Database) == "" {
		return ErrEmptyDatabaseName
	}

	return nil
}

// Option customizes internal client dependencies (primarily for tests).
type Option func(*clientDeps)

// Client wraps a MongoDB client with lifecycle and index helpers.
type Client struct {
	mu           sync.RWMutex
	client       *mongo.Client
	databaseName string
	cfg          Config
	deps         clientDeps
}

type clientDeps struct {
	connect     func(context.Context, *options.ClientOptions) (*mongo.Client, error)
	ping        func(context.Context, *mongo.Client) error
	disconnect  func(context.Context, *mongo.Client) error
	createIndex func(context.Context, *mongo.Client, string, string, mongo.IndexModel) error
}

func defaultDeps() clientDeps {
	return clientDeps{
		connect: func(ctx context.Context, clientOptions *options.ClientOptions) (*mongo.Client, error) {
			return mongo.Connect(ctx, clientOptions)
		},
		ping: func(ctx context.Context, client *mongo.Client) error {
			return client.Ping(ctx, nil)
		},
		disconnect: func(ctx context.Context, client *mongo.Client) error {
			return client.Disconnect(ctx)
		},
		createIndex: func(ctx context.Context, client *mongo.Client, database, collection string, index mongo.IndexModel) error {
			_, err := client.Database(database).Collection(collection).Indexes().CreateOne(ctx, index)

			return err
		},
	}
}

// NewClient validates config, connects to MongoDB, and returns a ready client.
func NewClient(ctx context.Context, cfg Config, opts ...Option) (*Client, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	deps := defaultDeps()

	for _, opt := range opts {
		if opt == nil {
			asserter := assert.New(ctx, cfg.Logger, "mongo", "NewClient")
			_ = asserter.Never(ctx, "nil mongo option received; skipping")

			continue
		}

		opt(&deps)
	}

	client := &Client{
		databaseName: cfg.Database,
		cfg:          cfg,
		deps:         deps,
	}

	if err := client.Connect(ctx); err != nil {
		return nil, err
	}

	return client, nil
}

// Connect establishes a MongoDB connection if one is not already open.
func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return ErrClientClosed
	}

	if ctx == nil {
		return ErrNilContext
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil
	}

	clientOptions := options.Client().ApplyURI(c.cfg.URI)

	serverSelectionTimeout := c.cfg.ServerSelectionTimeout
	if serverSelectionTimeout <= 0 {
		serverSelectionTimeout = defaultServerSelectionTimeout
	}

	heartbeatInterval := c.cfg.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}

	clientOptions.SetServerSelectionTimeout(serverSelectionTimeout)
	clientOptions.SetHeartbeatInterval(heartbeatInterval)

	if c.cfg.MaxPoolSize > 0 {
		clientOptions.SetMaxPoolSize(c.cfg.MaxPoolSize)
	}

	mongoClient, err := c.deps.connect(ctx, clientOptions)
	if err != nil {
		c.log(ctx, "mongo connect failed", log.Err(err))
		return fmt.Errorf("%w: %w", ErrConnect, err)
	}

	if mongoClient == nil {
		return ErrNilMongoClient
	}

	if err := c.deps.ping(ctx, mongoClient); err != nil {
		if disconnectErr := c.deps.disconnect(ctx, mongoClient); disconnectErr != nil {
			c.log(ctx, "failed to disconnect after ping failure", log.Err(disconnectErr))
		}

		c.log(ctx, "mongo ping failed", log.Err(err))

		return fmt.Errorf("%w: %w", ErrPing, err)
	}

	c.client = mongoClient

	return nil
}

// Client returns the underlying mongo client if connected.
func (c *Client) Client(ctx context.Context) (*mongo.Client, error) {
	if c == nil {
		return nil, ErrClientClosed
	}

	if ctx == nil {
		return nil, ErrNilContext
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, ErrClientClosed
	}

	return c.client, nil
}

// DatabaseName returns the configured database name.
func (c *Client) DatabaseName() (string, error) {
	if c == nil {
		asserter := assert.New(context.Background(), nil, "mongo", "DatabaseName")
		_ = asserter.Never(context.Background(), "mongo client receiver is nil")

		return "", ErrClientClosed
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.databaseName, nil
}

// Database returns the configured mongo database handle.
func (c *Client) Database(ctx context.Context) (*mongo.Database, error) {
	client, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	databaseName, err := c.DatabaseName()
	if err != nil {
		return nil, err
	}

	return client.Database(databaseName), nil
}

// Ping checks MongoDB availability using the active connection.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return ErrClientClosed
	}

	if ctx == nil {
		return ErrNilContext
	}

	client, err := c.Client(ctx)
	if err != nil {
		return err
	}

	if err := c.deps.ping(ctx, client); err != nil {
		return fmt.Errorf("%w: %w", ErrPing, err)
	}

	return nil
}

// Close releases the MongoDB connection.
// The client is marked as closed regardless of whether disconnect succeeds or fails.
// This prevents callers from retrying operations on a potentially half-closed client.
func (c *Client) Close(ctx context.Context) error {
	if c == nil {
		asserter := assert.New(context.Background(), nil, "mongo", "Close")
		_ = asserter.Never(context.Background(), "mongo client receiver is nil")

		return ErrClientClosed
	}

	if ctx == nil {
		return ErrNilContext
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	err := c.deps.disconnect(ctx, c.client)
	c.client = nil

	if err != nil {
		c.log(ctx, "mongo disconnect failed", log.Err(err))
		return fmt.Errorf("%w: %w", ErrDisconnect, err)
	}

	return nil
}

// EnsureIndexes creates indexes for a collection if they do not already exist.
func (c *Client) EnsureIndexes(ctx context.Context, collection string, indexes ...mongo.IndexModel) error {
	if c == nil {
		return ErrClientClosed
	}

	if ctx == nil {
		return ErrNilContext
	}

	if strings.TrimSpace(collection) == "" {
		return ErrEmptyCollectionName
	}

	if len(indexes) == 0 {
		return ErrEmptyIndexes
	}

	client, err := c.Client(ctx)
	if err != nil {
		return err
	}

	databaseName, err := c.DatabaseName()
	if err != nil {
		return err
	}

	for _, index := range indexes {
		fields := indexKeysString(index.Keys)
		c.log(ctx, "ensuring mongo index", log.String("collection", collection), log.String("fields", fields))

		if err := c.deps.createIndex(ctx, client, databaseName, collection, index); err != nil {
			c.logAtLevel(ctx, log.LevelWarn, "failed to create mongo index",
				log.String("collection", collection),
				log.String("fields", fields),
				log.Err(err),
			)

			return fmt.Errorf("%w: collection=%s fields=%s err=%w", ErrCreateIndex, collection, fields, err)
		}
	}

	return nil
}

func (c *Client) log(ctx context.Context, message string, fields ...log.Field) {
	c.logAtLevel(ctx, log.LevelDebug, message, fields...)
}

func (c *Client) logAtLevel(ctx context.Context, level log.Level, message string, fields ...log.Field) {
	if c == nil || c.cfg.Logger == nil {
		return
	}

	if !c.cfg.Logger.Enabled(level) {
		return
	}

	c.cfg.Logger.Log(ctx, level, message, fields...)
}

// indexKeysString returns a string representation of the index keys.
// It's used to log the index keys in a human-readable format.
func indexKeysString(keys any) string {
	switch k := keys.(type) {
	case bson.D:
		parts := make([]string, 0, len(k))
		for _, e := range k {
			parts = append(parts, e.Key)
		}

		return strings.Join(parts, ",")
	case bson.M:
		parts := make([]string, 0, len(k))
		for key := range k {
			parts = append(parts, key)
		}

		sort.Strings(parts)

		return strings.Join(parts, ",")
	default:
		return "<unknown>"
	}
}
