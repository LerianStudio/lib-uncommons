//go:build unit

package redis

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStandaloneConfig(addr string) Config {
	return Config{
		Topology: Topology{
			Standalone: &StandaloneTopology{Address: addr},
		},
		Logger: &log.NopLogger{},
	}
}

func TestClient_NewAndGetClient(t *testing.T) {
	mr := miniredis.RunT(t)

	client, err := New(context.Background(), newStandaloneConfig(mr.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	redisClient, err := client.GetClient(context.Background())
	require.NoError(t, err)

	require.NoError(t, redisClient.Set(context.Background(), "test:key", "value", 0).Err())
	value, err := redisClient.Get(context.Background(), "test:key").Result()
	require.NoError(t, err)
	assert.Equal(t, "value", value)
	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.True(t, connected)
}

func TestClient_New_InvalidConfig(t *testing.T) {
	validCert := base64.StdEncoding.EncodeToString(generateTestCertificatePEM(t))

	tests := []struct {
		name    string
		cfg     Config
		errText string
	}{
		{
			name:    "missing topology",
			cfg:     Config{Logger: &log.NopLogger{}},
			errText: "exactly one topology",
		},
		{
			name: "multiple topologies",
			cfg: Config{
				Topology: Topology{
					Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"},
					Cluster:    &ClusterTopology{Addresses: []string{"127.0.0.1:6379"}},
				},
				Logger: &log.NopLogger{},
			},
			errText: "exactly one topology",
		},
		{
			name: "gcp iam requires tls",
			cfg: Config{
				Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
				Auth: Auth{
					GCPIAM: &GCPIAMAuth{
						CredentialsBase64: "abc",
						ServiceAccount:    "svc@project.iam.gserviceaccount.com",
					},
				},
				Logger: &log.NopLogger{},
			},
			errText: "TLS must be configured",
		},
		{
			name: "gcp iam requires service account",
			cfg: Config{
				Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
				TLS:      &TLSConfig{CACertBase64: validCert},
				Auth: Auth{
					GCPIAM: &GCPIAMAuth{CredentialsBase64: "abc"},
				},
				Logger: &log.NopLogger{},
			},
			errText: "service account is required",
		},
		{
			name: "gcp iam service account cannot contain slash",
			cfg: Config{
				Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
				TLS:      &TLSConfig{CACertBase64: validCert},
				Auth: Auth{
					GCPIAM: &GCPIAMAuth{
						CredentialsBase64: "abc",
						ServiceAccount:    "projects/-/serviceAccounts/svc@project.iam.gserviceaccount.com",
					},
				},
				Logger: &log.NopLogger{},
			},
			errText: "cannot contain '/'",
		},
		{
			name: "gcp iam credentials required",
			cfg: Config{
				Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
				TLS:      &TLSConfig{CACertBase64: validCert},
				Auth: Auth{
					GCPIAM: &GCPIAMAuth{ServiceAccount: "svc@project.iam.gserviceaccount.com"},
				},
				Logger: &log.NopLogger{},
			},
			errText: "credentials are required",
		},
		{
			name: "tls requires ca cert",
			cfg: Config{
				Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
				TLS:      &TLSConfig{},
				Logger:   &log.NopLogger{},
			},
			errText: "TLS CA cert is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := New(context.Background(), test.cfg)
			require.Error(t, err)
			assert.Nil(t, client)
			assert.ErrorIs(t, err, ErrInvalidConfig)
			assert.Contains(t, err.Error(), test.errText)
		})
	}
}

func TestBuildTLSConfig(t *testing.T) {
	_, err := buildTLSConfig(TLSConfig{CACertBase64: "not-base64"})
	assert.Error(t, err)

	_, err = buildTLSConfig(TLSConfig{CACertBase64: base64.StdEncoding.EncodeToString([]byte("not-a-pem"))})
	assert.Error(t, err)

	cfg, err := buildTLSConfig(TLSConfig{
		CACertBase64: base64.StdEncoding.EncodeToString(generateTestCertificatePEM(t)),
		MinVersion:   tls.VersionTLS12,
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestClient_NilReceiverGuards(t *testing.T) {
	var client *Client

	err := client.Connect(context.Background())
	assert.ErrorIs(t, err, ErrNilClient)

	rdb, err := client.GetClient(context.Background())
	assert.ErrorIs(t, err, ErrNilClient)
	assert.Nil(t, rdb)

	err = client.Close()
	assert.ErrorIs(t, err, ErrNilClient)

	connected, err := client.IsConnected()
	assert.ErrorIs(t, err, ErrNilClient)
	assert.False(t, connected)
	assert.ErrorIs(t, client.LastRefreshError(), ErrNilClient)
}

func TestClient_StatusLifecycle(t *testing.T) {
	mr := miniredis.RunT(t)

	client, err := New(context.Background(), newStandaloneConfig(mr.Addr()))
	require.NoError(t, err)

	status, err := client.Status()
	require.NoError(t, err)
	assert.True(t, status.Connected)
	assert.Nil(t, status.LastRefreshError)

	require.NoError(t, client.Close())
	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.False(t, connected)
}

func TestClient_RefreshLoop_DoesNotDuplicateGoroutines(t *testing.T) {
	validCert := base64.StdEncoding.EncodeToString(generateTestCertificatePEM(t))
	normalized, err := normalizeConfig(Config{
		Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
		TLS:      &TLSConfig{CACertBase64: validCert},
		Auth: Auth{GCPIAM: &GCPIAMAuth{
			CredentialsBase64:       base64.StdEncoding.EncodeToString([]byte("{}")),
			ServiceAccount:          "svc@project.iam.gserviceaccount.com",
			RefreshEvery:            time.Millisecond,
			RefreshCheckInterval:    time.Millisecond,
			RefreshOperationTimeout: time.Second,
		}},
		Logger: &log.NopLogger{},
	})
	require.NoError(t, err)

	var calls int32
	client := &Client{
		cfg:    normalized,
		logger: normalized.Logger,
		tokenRetriever: func(ctx context.Context) (string, error) {
			atomic.AddInt32(&calls, 1)
			<-ctx.Done()

			return "", ctx.Err()
		},
		reconnectFn: func(context.Context) error { return nil },
	}

	client.mu.Lock()
	client.lastRefresh = time.Now().Add(-time.Hour)
	client.startRefreshLoopLocked()
	client.startRefreshLoopLocked()
	client.mu.Unlock()

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&calls) >= 1
	}, 200*time.Millisecond, 10*time.Millisecond)

	require.NoError(t, client.Close())
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestClient_RefreshStatusErrorAndRecovery(t *testing.T) {
	validCert := base64.StdEncoding.EncodeToString(generateTestCertificatePEM(t))
	normalized, err := normalizeConfig(Config{
		Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
		TLS:      &TLSConfig{CACertBase64: validCert},
		Auth: Auth{GCPIAM: &GCPIAMAuth{
			CredentialsBase64:       base64.StdEncoding.EncodeToString([]byte("{}")),
			ServiceAccount:          "svc@project.iam.gserviceaccount.com",
			RefreshEvery:            time.Millisecond,
			RefreshCheckInterval:    time.Millisecond,
			RefreshOperationTimeout: time.Second,
		}},
		Logger: &log.NopLogger{},
	})
	require.NoError(t, err)

	firstErr := errors.New("token refresh failed")
	var shouldFail atomic.Bool
	shouldFail.Store(true)

	client := &Client{
		cfg:    normalized,
		logger: normalized.Logger,
		tokenRetriever: func(context.Context) (string, error) {
			if shouldFail.Load() {
				return "", firstErr
			}

			return "token", nil
		},
		reconnectFn: func(context.Context) error { return nil },
	}

	client.mu.Lock()
	client.lastRefresh = time.Now().Add(-time.Hour)
	client.startRefreshLoopLocked()
	client.mu.Unlock()

	require.Eventually(t, func() bool {
		return errors.Is(client.LastRefreshError(), firstErr)
	}, 500*time.Millisecond, 10*time.Millisecond)

	shouldFail.Store(false)

	require.Eventually(t, func() bool {
		return client.LastRefreshError() == nil
	}, 500*time.Millisecond, 10*time.Millisecond)

	require.NoError(t, client.Close())
}

func TestClient_Connect_ReconnectClosesPreviousClient(t *testing.T) {
	mr := miniredis.RunT(t)

	client, err := New(context.Background(), newStandaloneConfig(mr.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	firstClient, err := client.GetClient(context.Background())
	require.NoError(t, err)

	require.NoError(t, client.Connect(context.Background()))

	secondClient, err := client.GetClient(context.Background())
	require.NoError(t, err)
	assert.NotSame(t, firstClient, secondClient)

	_, err = firstClient.Ping(context.Background()).Result()
	assert.Error(t, err)
}

func TestClient_ReconnectFailure_PreservesOldClient(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr() // capture address before closing

	// Connect a working standalone client (no IAM -- we test reconnect directly).
	client, err := New(context.Background(), newStandaloneConfig(addr))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	// Verify initial connectivity.
	rdb, err := client.GetClient(context.Background())
	require.NoError(t, err)
	require.NoError(t, rdb.Set(context.Background(), "preserve:key", "before", 0).Err())

	// Shut down miniredis so the new client Ping fails during reconnect.
	mr.Close()

	// Simulate a reconnect failure.
	client.mu.Lock()
	err = client.reconnectLocked(context.Background())
	client.mu.Unlock()

	// reconnectLocked must return an error (Ping against closed server fails).
	require.Error(t, err, "reconnectLocked should fail when new client cannot Ping")

	// The old client must still be set and marked connected.
	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.True(t, connected, "client must remain connected after failed reconnect")

	// Restart miniredis on the same address so the OLD preserved client can work again.
	mr2 := miniredis.NewMiniRedis()
	require.NoError(t, mr2.StartAddr(addr))
	t.Cleanup(mr2.Close)

	// The preserved old client must still be usable.
	rdb2, err := client.GetClient(context.Background())
	require.NoError(t, err)
	require.NoError(t, rdb2.Set(context.Background(), "preserve:key", "still-works", 0).Err())

	val, err := rdb2.Get(context.Background(), "preserve:key").Result()
	require.NoError(t, err)
	assert.Equal(t, "still-works", val)
}

func TestClient_ReconnectFailure_IAMRefreshLoopPreservesClient(t *testing.T) {
	validCert := base64.StdEncoding.EncodeToString(generateTestCertificatePEM(t))
	normalized, err := normalizeConfig(Config{
		Topology: Topology{Standalone: &StandaloneTopology{Address: "127.0.0.1:6379"}},
		TLS:      &TLSConfig{CACertBase64: validCert},
		Auth: Auth{GCPIAM: &GCPIAMAuth{
			CredentialsBase64:       base64.StdEncoding.EncodeToString([]byte("{}")),
			ServiceAccount:          "svc@project.iam.gserviceaccount.com",
			RefreshEvery:            time.Millisecond,
			RefreshCheckInterval:    time.Millisecond,
			RefreshOperationTimeout: time.Second,
		}},
		Logger: &log.NopLogger{},
	})
	require.NoError(t, err)

	reconnectErr := errors.New("simulated reconnect failure")
	var reconnectShouldFail atomic.Bool
	reconnectShouldFail.Store(true)

	var reconnectCalls atomic.Int32
	var tokenAtReconnect atomic.Value

	client := &Client{
		cfg:       normalized,
		logger:    normalized.Logger,
		connected: true,
		token:     "original-working-token",
		tokenRetriever: func(context.Context) (string, error) {
			return "new-refreshed-token", nil
		},
		reconnectFn: func(ctx context.Context) error {
			reconnectCalls.Add(1)

			// Capture the token at the time of reconnect attempt for verification.
			tokenAtReconnect.Store("called")

			if reconnectShouldFail.Load() {
				return reconnectErr
			}

			return nil
		},
	}

	client.mu.Lock()
	client.lastRefresh = time.Now().Add(-time.Hour)
	client.startRefreshLoopLocked()
	client.mu.Unlock()

	// Wait for at least one failed reconnect attempt.
	require.Eventually(t, func() bool {
		return reconnectCalls.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)

	// Verify: the refresh error is recorded.
	require.Eventually(t, func() bool {
		return client.LastRefreshError() != nil
	}, 500*time.Millisecond, 5*time.Millisecond)
	assert.ErrorIs(t, client.LastRefreshError(), reconnectErr)

	// Verify: the token is rolled back to the original after failed reconnect.
	client.mu.RLock()
	currentToken := client.token
	client.mu.RUnlock()
	assert.Equal(t, "original-working-token", currentToken,
		"token must be rolled back to original after failed reconnect")

	// Now allow reconnect to succeed.
	reconnectShouldFail.Store(false)

	// Wait for recovery.
	require.Eventually(t, func() bool {
		return client.LastRefreshError() == nil
	}, 500*time.Millisecond, 5*time.Millisecond)

	// After successful reconnect, the new token should be in place.
	client.mu.RLock()
	recoveredToken := client.token
	client.mu.RUnlock()
	assert.Equal(t, "new-refreshed-token", recoveredToken,
		"token must be updated after successful reconnect")

	require.NoError(t, client.Close())
}

func TestClient_ReconnectSuccess_SwapsClient(t *testing.T) {
	mr := miniredis.RunT(t)

	client, err := New(context.Background(), newStandaloneConfig(mr.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	// Grab reference to the original underlying client.
	rdb1, err := client.GetClient(context.Background())
	require.NoError(t, err)

	// Successful reconnect should swap the client.
	client.mu.Lock()
	err = client.reconnectLocked(context.Background())
	client.mu.Unlock()
	require.NoError(t, err)

	rdb2, err := client.GetClient(context.Background())
	require.NoError(t, err)

	// The client reference must have changed.
	assert.NotSame(t, rdb1, rdb2, "successful reconnect must swap to new client")

	// Old client must be closed.
	_, err = rdb1.Ping(context.Background()).Result()
	assert.Error(t, err, "old client must be closed after successful reconnect")

	// New client must work.
	require.NoError(t, rdb2.Set(context.Background(), "swap:key", "works", 0).Err())
	val, err := rdb2.Get(context.Background(), "swap:key").Result()
	require.NoError(t, err)
	assert.Equal(t, "works", val)

	connected, err := client.IsConnected()
	require.NoError(t, err)
	assert.True(t, connected)
}

func generateTestCertificatePEM(t *testing.T) []byte {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "redis-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}
