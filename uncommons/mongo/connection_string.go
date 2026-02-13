package mongo

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrInvalidScheme        = errors.New("invalid mongo uri scheme")
	ErrEmptyHost            = errors.New("mongo uri host cannot be empty")
	ErrInvalidPort          = errors.New("mongo uri port is invalid")
	ErrPortNotAllowedForSRV = errors.New("port cannot be set for mongodb+srv")
	ErrPasswordWithoutUser  = errors.New("password requires username")
)

// URIConfig contains the components used to build a MongoDB URI.
type URIConfig struct {
	Scheme   string
	Username string
	Password string
	Host     string
	Port     string
	Database string
	Query    url.Values
}

// BuildURI validates URIConfig and returns a canonical MongoDB connection URI.
func BuildURI(cfg URIConfig) (string, error) {
	scheme := strings.TrimSpace(cfg.Scheme)
	host := strings.TrimSpace(cfg.Host)
	port := strings.TrimSpace(cfg.Port)

	if scheme != "mongodb" && scheme != "mongodb+srv" {
		return "", ErrInvalidScheme
	}

	if host == "" {
		return "", ErrEmptyHost
	}

	if cfg.Username == "" && cfg.Password != "" {
		return "", ErrPasswordWithoutUser
	}

	if scheme == "mongodb+srv" && port != "" {
		return "", ErrPortNotAllowedForSRV
	}

	if scheme == "mongodb" && port != "" {
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", ErrInvalidPort
		}
	}

	uri := &url.URL{Scheme: scheme}

	if port != "" {
		uri.Host = host + ":" + port
	} else {
		uri.Host = host
	}

	if cfg.Username != "" {
		// url.UserPassword encodes username:password in the URI.
		// When Password is empty, this produces "username:@" which is valid per RFC 3986.
		uri.User = url.UserPassword(cfg.Username, cfg.Password)
	}

	database := strings.TrimSpace(cfg.Database)
	if database == "" {
		uri.Path = "/"
	} else {
		uri.Path = "/" + url.PathEscape(database)
	}

	if len(cfg.Query) > 0 {
		uri.RawQuery = cfg.Query.Encode()
	}

	return uri.String(), nil
}
