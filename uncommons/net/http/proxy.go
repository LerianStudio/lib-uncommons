package http

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	constant "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
)

var (
	ErrInvalidProxyTarget     = errors.New("invalid proxy target")
	ErrUntrustedProxyScheme   = errors.New("untrusted proxy scheme")
	ErrUntrustedProxyHost     = errors.New("untrusted proxy host")
	ErrUnsafeProxyDestination = errors.New("unsafe proxy destination")
	ErrNilProxyRequest        = errors.New("proxy request cannot be nil")
	ErrNilProxyResponse       = errors.New("proxy response writer cannot be nil")
)

// ReverseProxyPolicy defines strict trust boundaries for reverse proxy targets.
type ReverseProxyPolicy struct {
	AllowedSchemes []string
	// AllowedHosts restricts proxy targets to the listed hostnames (case-insensitive).
	// An empty or nil slice rejects all hosts (secure-by-default), matching AllowedSchemes behavior.
	// Callers must explicitly populate this to permit proxy targets.
	// See isAllowedHost and ErrUntrustedProxyHost for enforcement details.
	AllowedHosts            []string
	AllowUnsafeDestinations bool
}

// DefaultReverseProxyPolicy returns a strict-by-default reverse proxy policy.
func DefaultReverseProxyPolicy() ReverseProxyPolicy {
	return ReverseProxyPolicy{
		AllowedSchemes:          []string{"https"},
		AllowedHosts:            nil,
		AllowUnsafeDestinations: false,
	}
}

// ServeReverseProxy serves a reverse proxy for a given URL, enforcing policy checks.
func ServeReverseProxy(target string, policy ReverseProxyPolicy, res http.ResponseWriter, req *http.Request) error {
	if req == nil {
		return ErrNilProxyRequest
	}

	if res == nil {
		return ErrNilProxyResponse
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		return ErrInvalidProxyTarget
	}

	if err := validateProxyTarget(targetURL, policy); err != nil {
		return err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Update the headers to allow for SSL redirection
	req.URL.Host = targetURL.Host
	req.URL.Scheme = targetURL.Scheme
	req.Header.Set(constant.HeaderForwardedHost, req.Header.Get(constant.HeaderHost))
	req.Host = targetURL.Host

	proxy.ServeHTTP(res, req)

	return nil
}

func validateProxyTarget(targetURL *url.URL, policy ReverseProxyPolicy) error {
	if targetURL == nil || targetURL.Scheme == "" || targetURL.Host == "" {
		return ErrInvalidProxyTarget
	}

	if !isAllowedScheme(targetURL.Scheme, policy.AllowedSchemes) {
		return ErrUntrustedProxyScheme
	}

	hostname := targetURL.Hostname()
	if hostname == "" {
		return ErrInvalidProxyTarget
	}

	if strings.EqualFold(hostname, "localhost") && !policy.AllowUnsafeDestinations {
		return ErrUnsafeProxyDestination
	}

	if !isAllowedHost(hostname, policy.AllowedHosts) {
		return ErrUntrustedProxyHost
	}

	if ip := net.ParseIP(hostname); ip != nil && isUnsafeIP(ip) && !policy.AllowUnsafeDestinations {
		return ErrUnsafeProxyDestination
	}

	return nil
}

func isAllowedScheme(scheme string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}

	for _, candidate := range allowed {
		if strings.EqualFold(scheme, candidate) {
			return true
		}
	}

	return false
}

func isAllowedHost(host string, allowedHosts []string) bool {
	if len(allowedHosts) == 0 {
		return false
	}

	for _, candidate := range allowedHosts {
		if strings.EqualFold(host, candidate) {
			return true
		}
	}

	return false
}

func isUnsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast()
}
