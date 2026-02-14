package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	iamcredentials "cloud.google.com/go/iam/credentials/apiv1"
	iamcredentialspb "cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/assert"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/runtime"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	gcpScope                = "https://www.googleapis.com/auth/cloud-platform"
	gcpServiceAccountPrefix = "projects/-/serviceAccounts/"

	defaultTokenLifetime           = 1 * time.Hour
	defaultRefreshEvery            = 50 * time.Minute
	defaultRefreshCheckInterval    = 10 * time.Second
	defaultRefreshOperationTimeout = 15 * time.Second
)

var (
	ErrNilClient     = errors.New("redis client is nil")
	ErrInvalidConfig = errors.New("invalid redis config")
)

// Config defines Redis client topology, auth, TLS, and connection settings.
type Config struct {
	Topology Topology
	TLS      *TLSConfig
	Auth     Auth
	Options  ConnectionOptions
	Logger   log.Logger
}

// Topology selects exactly one Redis deployment mode.
type Topology struct {
	Standalone *StandaloneTopology
	Sentinel   *SentinelTopology
	Cluster    *ClusterTopology
}

// StandaloneTopology configures single-node Redis access.
type StandaloneTopology struct {
	Address string
}

// SentinelTopology configures Redis Sentinel access.
type SentinelTopology struct {
	Addresses  []string
	MasterName string
}

// ClusterTopology configures Redis cluster access.
type ClusterTopology struct {
	Addresses []string
}

// TLSConfig configures TLS validation for Redis connections.
type TLSConfig struct {
	CACertBase64 string
	MinVersion   uint16
}

// Auth selects one Redis authentication strategy.
type Auth struct {
	StaticPassword *StaticPasswordAuth
	GCPIAM         *GCPIAMAuth
}

// StaticPasswordAuth authenticates using a static password.
type StaticPasswordAuth struct {
	Password string
}

// GCPIAMAuth authenticates with short-lived GCP IAM access tokens.
type GCPIAMAuth struct {
	CredentialsBase64       string
	ServiceAccount          string
	TokenLifetime           time.Duration
	RefreshEvery            time.Duration
	RefreshCheckInterval    time.Duration
	RefreshOperationTimeout time.Duration
}

// ConnectionOptions configures protocol, timeouts, pools, and retries.
type ConnectionOptions struct {
	DB              int
	Protocol        int
	PoolSize        int
	MinIdleConns    int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	DialTimeout     time.Duration
	PoolTimeout     time.Duration
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
}

// Status reports client connectivity and IAM refresh loop health.
type Status struct {
	Connected          bool
	LastRefreshError   error
	LastRefreshAt      time.Time
	RefreshLoopRunning bool
}

// Client wraps a redis.UniversalClient with reconnection and IAM token refresh logic.
type Client struct {
	mu          sync.RWMutex
	cfg         Config
	logger      log.Logger
	client      redis.UniversalClient
	connected   bool
	token       string
	lastRefresh time.Time
	refreshErr  error

	refreshCancel      context.CancelFunc
	refreshLoopRunning bool
	refreshGeneration  uint64

	// test hooks
	tokenRetriever func(ctx context.Context) (string, error)
	reconnectFn    func(ctx context.Context) error
}

// New validates config, connects to Redis, and returns a ready client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	c := &Client{
		cfg:    normalized,
		logger: normalized.Logger,
	}

	if err := c.Connect(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

// Connect establishes a Redis connection using the current client configuration.
func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return ErrNilClient
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked(ctx)
}

// GetClient returns a connected redis client, reconnecting on demand if needed.
func (c *Client) GetClient(ctx context.Context) (redis.UniversalClient, error) {
	if c == nil {
		return nil, ErrNilClient
	}

	c.mu.RLock()

	if c.client != nil {
		client := c.client
		c.mu.RUnlock()

		return client, nil
	}

	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return c.client, nil
	}

	if err := c.connectLocked(ctx); err != nil {
		return nil, err
	}

	return c.client, nil
}

// Close stops background refresh and closes the underlying Redis client.
func (c *Client) Close() error {
	if c == nil {
		return ErrNilClient
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopRefreshLoopLocked()

	return c.closeClientLocked()
}

// Status returns a snapshot of connectivity and token refresh state.
func (c *Client) Status() (Status, error) {
	if c == nil {
		asserter := assert.New(context.Background(), nil, "redis", "Status")
		_ = asserter.Never(context.Background(), "redis client receiver is nil")

		return Status{}, ErrNilClient
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return Status{
		Connected:          c.connected,
		LastRefreshError:   c.refreshErr,
		LastRefreshAt:      c.lastRefresh,
		RefreshLoopRunning: c.refreshLoopRunning,
	}, nil
}

// IsConnected reports whether the underlying client is currently connected.
func (c *Client) IsConnected() (bool, error) {
	status, err := c.Status()
	if err != nil {
		return false, err
	}

	return status.Connected, nil
}

// LastRefreshError returns the latest IAM refresh/reconnect error.
func (c *Client) LastRefreshError() error {
	if c == nil {
		return ErrNilClient
	}

	status, err := c.Status()
	if err != nil {
		return err
	}

	return status.LastRefreshError
}

func (c *Client) connectLocked(ctx context.Context) error {
	// Config validation is performed by New/normalizeConfig at construction time.
	// Direct Connect() callers should only use properly-constructed Clients.
	c.logger.Log(ctx, log.LevelInfo, "connecting to Redis/Valkey")

	if c.usesGCPIAM() && c.token == "" {
		token, err := c.retrieveToken(ctx)
		if err != nil {
			c.logger.Log(ctx, log.LevelError, "initial token retrieval failed", log.Err(err))

			return err
		}

		c.token = token
	}

	if c.client != nil {
		if err := c.closeClientLocked(); err != nil {
			c.logger.Log(ctx, log.LevelWarn, "close before connect failed", log.Err(err))
		}
	}

	if err := c.connectClientLocked(ctx); err != nil {
		return err
	}

	if c.usesGCPIAM() {
		c.lastRefresh = time.Now()
		c.startRefreshLoopLocked()
	}

	return nil
}

func (c *Client) connectClientLocked(ctx context.Context) error {
	opts, err := c.buildUniversalOptionsLocked()
	if err != nil {
		return err
	}

	rdb := redis.NewUniversalClient(opts)
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		_ = rdb.Close()

		c.logger.Log(ctx, log.LevelError, "redis ping failed", log.Err(err))
		c.connected = false

		return err
	}

	c.client = rdb
	c.connected = true
	c.refreshErr = nil

	switch rdb.(type) {
	case *redis.ClusterClient:
		c.logger.Log(ctx, log.LevelInfo, "connected to Redis/Valkey in cluster mode")
	case *redis.Client:
		c.logger.Log(ctx, log.LevelInfo, "connected to Redis/Valkey in standalone mode")
	case *redis.Ring:
		c.logger.Log(ctx, log.LevelInfo, "connected to Redis/Valkey in sentinel mode")
	default:
		c.logger.Log(ctx, log.LevelWarn, "connected to Redis/Valkey in unknown mode")
	}

	return nil
}

func (c *Client) closeClientLocked() error {
	if c.client == nil {
		return nil
	}

	err := c.client.Close()
	c.client = nil
	c.connected = false

	return err
}

func (c *Client) buildUniversalOptionsLocked() (*redis.UniversalOptions, error) {
	o := c.cfg.Options
	opts := &redis.UniversalOptions{
		DB:              o.DB,
		Protocol:        o.Protocol,
		PoolSize:        o.PoolSize,
		MinIdleConns:    o.MinIdleConns,
		ReadTimeout:     o.ReadTimeout,
		WriteTimeout:    o.WriteTimeout,
		DialTimeout:     o.DialTimeout,
		PoolTimeout:     o.PoolTimeout,
		MaxRetries:      o.MaxRetries,
		MinRetryBackoff: o.MinRetryBackoff,
		MaxRetryBackoff: o.MaxRetryBackoff,
	}

	if c.cfg.Topology.Standalone != nil {
		opts.Addrs = []string{c.cfg.Topology.Standalone.Address}
	}

	if c.cfg.Topology.Sentinel != nil {
		opts.Addrs = c.cfg.Topology.Sentinel.Addresses
		opts.MasterName = c.cfg.Topology.Sentinel.MasterName
	}

	if c.cfg.Topology.Cluster != nil {
		opts.Addrs = c.cfg.Topology.Cluster.Addresses
	}

	if c.cfg.Auth.StaticPassword != nil {
		opts.Password = c.cfg.Auth.StaticPassword.Password
	}

	if c.usesGCPIAM() {
		opts.Username = "default"
		opts.Password = c.token
	}

	if c.cfg.TLS != nil {
		tlsCfg, err := buildTLSConfig(*c.cfg.TLS)
		if err != nil {
			return nil, err
		}

		opts.TLSConfig = tlsCfg
	}

	return opts, nil
}

func (c *Client) retrieveToken(ctx context.Context) (string, error) {
	if c == nil {
		return "", ErrNilClient
	}

	if c.tokenRetriever != nil {
		return c.tokenRetriever(ctx)
	}

	auth := c.cfg.Auth.GCPIAM
	if auth == nil {
		return "", errors.New("GCP IAM auth is not configured")
	}

	credentialsJSON, err := base64.StdEncoding.DecodeString(auth.CredentialsBase64)
	if err != nil {
		c.logger.Log(ctx, log.LevelError, "failed to decode base64 credentials", log.Err(err))

		return "", err
	}

	creds, err := google.CredentialsFromJSONWithType(ctx, credentialsJSON, google.ServiceAccount)
	if err != nil {
		return "", fmt.Errorf("parsing credentials JSON: %w", err)
	}

	client, err := iamcredentials.NewIamCredentialsClient(ctx, option.WithCredentials(creds))
	if err != nil {
		return "", fmt.Errorf("creating IAM credentials client: %w", err)
	}
	defer client.Close()

	resp, err := client.GenerateAccessToken(ctx, &iamcredentialspb.GenerateAccessTokenRequest{
		Name:     gcpServiceAccountPrefix + auth.ServiceAccount,
		Scope:    []string{gcpScope},
		Lifetime: durationpb.New(auth.TokenLifetime),
	})
	if err != nil {
		return "", fmt.Errorf("problem generating access token: %w", err)
	}

	if resp == nil {
		return "", errors.New("generate access token returned nil response")
	}

	return resp.AccessToken, nil
}

func (c *Client) refreshTokenLoop(ctx context.Context) {
	if c == nil {
		return
	}

	auth := c.cfg.Auth.GCPIAM

	ticker := time.NewTicker(auth.RefreshCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				c.mu.RLock()
				lastRefresh := c.lastRefresh
				c.mu.RUnlock()

				if !time.Now().After(lastRefresh.Add(auth.RefreshEvery)) {
					return
				}

				refreshCtx, cancel := context.WithTimeout(ctx, auth.RefreshOperationTimeout)
				defer cancel()

				token, err := c.retrieveToken(refreshCtx)
				if err != nil {
					c.mu.Lock()
					c.refreshErr = err
					c.logger.Log(refreshCtx, log.LevelWarn, "IAM token refresh failed", log.Err(err))
					c.mu.Unlock()

					return
				}

				c.mu.Lock()
				oldToken := c.token
				c.token = token

				reconnectFn := c.reconnectFn
				if reconnectFn == nil {
					reconnectFn = c.reconnectLocked
				}

				if err := reconnectFn(refreshCtx); err != nil {
					c.refreshErr = err
					// Restore old token: reconnect failed, so the new token is useless
					// and the old client (if any) is still using the previous token.
					c.token = oldToken
					c.logger.Log(refreshCtx, log.LevelError, "failed to reconnect after IAM token refresh, keeping existing client", log.Err(err))
					c.mu.Unlock()

					return
				}

				c.lastRefresh = time.Now()
				c.refreshErr = nil
				c.logger.Log(refreshCtx, log.LevelInfo, "IAM token refreshed")
				c.mu.Unlock()
			}()

		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) reconnectLocked(ctx context.Context) error {
	// Build new client options with the refreshed token.
	opts, err := c.buildUniversalOptionsLocked()
	if err != nil {
		c.logger.Log(ctx, log.LevelError, "failed to build options for reconnect", log.Err(err))

		return err
	}

	// Create and verify the new client BEFORE touching the old one.
	newClient := redis.NewUniversalClient(opts)

	if _, err := newClient.Ping(ctx).Result(); err != nil {
		_ = newClient.Close()

		c.logger.Log(ctx, log.LevelError, "new client ping failed during reconnect, keeping existing client", log.Err(err))

		return err
	}

	// New client is verified. Swap atomically: close old, assign new.
	oldClient := c.client

	c.client = newClient
	c.connected = true
	c.refreshErr = nil

	if oldClient != nil {
		if err := oldClient.Close(); err != nil {
			c.logger.Log(ctx, log.LevelWarn, "failed to close previous client after successful reconnect", log.Err(err))
		}
	}

	return nil
}

func (c *Client) startRefreshLoopLocked() {
	if !c.usesGCPIAM() || c.refreshLoopRunning {
		return
	}

	refreshCtx, cancel := context.WithCancel(context.Background())
	c.refreshGeneration++
	generation := c.refreshGeneration
	c.refreshCancel = cancel
	c.refreshLoopRunning = true

	runtime.SafeGoWithContextAndComponent(
		refreshCtx,
		c.logger,
		"redis",
		"iam_refresh_loop",
		runtime.KeepRunning,
		func(_ context.Context) {
			c.refreshTokenLoop(refreshCtx)

			c.mu.Lock()
			defer c.mu.Unlock()

			if c.refreshGeneration == generation {
				c.refreshCancel = nil
				c.refreshLoopRunning = false
			}
		},
	)
}

func (c *Client) stopRefreshLoopLocked() {
	if c.refreshCancel != nil {
		c.refreshCancel()
		c.refreshCancel = nil
	}

	c.refreshLoopRunning = false
}

func (c *Client) usesGCPIAM() bool {
	return c.cfg.Auth.GCPIAM != nil
}

func normalizeConfig(cfg Config) (Config, error) {
	normalizeLoggerDefault(&cfg)
	normalizeConnectionOptionsDefaults(&cfg.Options)
	normalizeTLSDefaults(cfg.TLS)
	normalizeGCPIAMDefaults(cfg.Auth.GCPIAM)

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func normalizeLoggerDefault(cfg *Config) {
	if cfg.Logger == nil {
		cfg.Logger = &log.NopLogger{}
	}
}

func normalizeConnectionOptionsDefaults(options *ConnectionOptions) {
	if options.PoolSize == 0 {
		options.PoolSize = 10
	}

	if options.ReadTimeout == 0 {
		options.ReadTimeout = 3 * time.Second
	}

	if options.WriteTimeout == 0 {
		options.WriteTimeout = 3 * time.Second
	}

	if options.DialTimeout == 0 {
		options.DialTimeout = 5 * time.Second
	}

	if options.PoolTimeout == 0 {
		options.PoolTimeout = 2 * time.Second
	}

	if options.MaxRetries == 0 {
		options.MaxRetries = 3
	}

	if options.MinRetryBackoff == 0 {
		options.MinRetryBackoff = 8 * time.Millisecond
	}

	if options.MaxRetryBackoff == 0 {
		options.MaxRetryBackoff = 1 * time.Second
	}
}

func normalizeTLSDefaults(tlsCfg *TLSConfig) {
	if tlsCfg != nil && tlsCfg.MinVersion == 0 {
		tlsCfg.MinVersion = tls.VersionTLS12
	}
}

func normalizeGCPIAMDefaults(auth *GCPIAMAuth) {
	if auth == nil {
		return
	}

	if auth.TokenLifetime == 0 {
		auth.TokenLifetime = defaultTokenLifetime
	}

	if auth.RefreshEvery == 0 {
		auth.RefreshEvery = defaultRefreshEvery
	}

	if auth.RefreshCheckInterval == 0 {
		auth.RefreshCheckInterval = defaultRefreshCheckInterval
	}

	if auth.RefreshOperationTimeout == 0 {
		auth.RefreshOperationTimeout = defaultRefreshOperationTimeout
	}
}

func validateConfig(cfg Config) error {
	if err := validateTopology(cfg.Topology); err != nil {
		return err
	}

	if cfg.Auth.StaticPassword != nil && cfg.Auth.GCPIAM != nil {
		return configError("only one auth strategy can be configured")
	}

	if cfg.TLS != nil && strings.TrimSpace(cfg.TLS.CACertBase64) == "" {
		return configError("TLS CA cert is required when TLS is configured")
	}

	if cfg.Auth.GCPIAM == nil {
		return nil
	}

	if cfg.TLS == nil {
		return configError("TLS must be configured when GCP IAM auth is enabled")
	}

	if strings.TrimSpace(cfg.Auth.GCPIAM.ServiceAccount) == "" {
		return configError("service account is required for GCP IAM auth")
	}

	if strings.Contains(cfg.Auth.GCPIAM.ServiceAccount, "/") {
		return configError("service account cannot contain '/' characters")
	}

	if strings.TrimSpace(cfg.Auth.GCPIAM.CredentialsBase64) == "" {
		return configError("credentials are required for GCP IAM auth")
	}

	return nil
}

func validateTopology(topology Topology) error {
	count := 0

	if topology.Standalone != nil {
		count++

		if strings.TrimSpace(topology.Standalone.Address) == "" {
			return configError("standalone address is required")
		}
	}

	if topology.Sentinel != nil {
		count++

		if len(topology.Sentinel.Addresses) == 0 {
			return configError("sentinel addresses are required")
		}

		if strings.TrimSpace(topology.Sentinel.MasterName) == "" {
			return configError("sentinel master name is required")
		}

		for _, address := range topology.Sentinel.Addresses {
			if strings.TrimSpace(address) == "" {
				return configError("sentinel addresses cannot be empty")
			}
		}
	}

	if topology.Cluster != nil {
		count++

		if len(topology.Cluster.Addresses) == 0 {
			return configError("cluster addresses are required")
		}

		for _, address := range topology.Cluster.Addresses {
			if strings.TrimSpace(address) == "" {
				return configError("cluster addresses cannot be empty")
			}
		}
	}

	if count != 1 {
		return configError("exactly one topology must be configured")
	}

	return nil
}

func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	caCert, err := base64.StdEncoding.DecodeString(cfg.CACertBase64)
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("adding CA cert failed")
	}

	return &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: cfg.MinVersion,
	}, nil
}

func configError(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, msg)
}
