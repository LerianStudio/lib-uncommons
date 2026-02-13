package mongo

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildURI_SuccessCases(t *testing.T) {
	t.Parallel()

	t.Run("mongodb with auth, port, database and query", func(t *testing.T) {
		t.Parallel()

		query := url.Values{}
		query.Set("authSource", "admin")
		query.Set("replicaSet", "rs0")

		uri, err := BuildURI(URIConfig{
			Scheme:   "mongodb",
			Username: "dbuser",
			Password: "p@ss:word/123",
			Host:     "localhost",
			Port:     "27017",
			Database: "ledger",
			Query:    query,
		})
		require.NoError(t, err)
		assert.Equal(t, "mongodb://dbuser:p%40ss%3Aword%2F123@localhost:27017/ledger?authSource=admin&replicaSet=rs0", uri)
	})

	t.Run("mongodb+srv omits port", func(t *testing.T) {
		t.Parallel()

		query := url.Values{}
		query.Set("retryWrites", "true")
		query.Set("w", "majority")

		uri, err := BuildURI(URIConfig{
			Scheme:   "mongodb+srv",
			Username: "user",
			Password: "secret",
			Host:     "cluster.mongodb.net",
			Database: "banking",
			Query:    query,
		})
		require.NoError(t, err)
		assert.Equal(t, "mongodb+srv://user:secret@cluster.mongodb.net/banking?retryWrites=true&w=majority", uri)
	})

	t.Run("without credentials defaults to root path", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{
			Scheme: "mongodb",
			Host:   "127.0.0.1",
			Port:   "27017",
		})
		require.NoError(t, err)
		assert.Equal(t, "mongodb://127.0.0.1:27017/", uri)
	})

	t.Run("username without password", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{
			Scheme:   "mongodb",
			Username: "readonly",
			Host:     "localhost",
			Port:     "27017",
		})
		require.NoError(t, err)
		assert.Equal(t, "mongodb://readonly:@localhost:27017/", uri)
	})
}

func TestBuildURI_Validation(t *testing.T) {
	t.Parallel()

	t.Run("invalid scheme", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{Scheme: "postgres", Host: "localhost"})
		assert.Empty(t, uri)
		assert.ErrorIs(t, err, ErrInvalidScheme)
	})

	t.Run("empty host", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{Scheme: "mongodb", Host: "  "})
		assert.Empty(t, uri)
		assert.ErrorIs(t, err, ErrEmptyHost)
	})

	t.Run("invalid port", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{Scheme: "mongodb", Host: "localhost", Port: "70000"})
		assert.Empty(t, uri)
		assert.ErrorIs(t, err, ErrInvalidPort)
	})

	t.Run("srv port is forbidden", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{Scheme: "mongodb+srv", Host: "cluster.mongodb.net", Port: "27017"})
		assert.Empty(t, uri)
		assert.ErrorIs(t, err, ErrPortNotAllowedForSRV)
	})

	t.Run("password without username", func(t *testing.T) {
		t.Parallel()

		uri, err := BuildURI(URIConfig{Scheme: "mongodb", Host: "localhost", Password: "secret"})
		assert.Empty(t, uri)
		assert.ErrorIs(t, err, ErrPasswordWithoutUser)
	})
}
