package common

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// SessionCookieTrustedURLs is also used as the trusted browser-origin list for
// authenticated WebSocket handshakes. Legacy deployments may configure the
// equivalent list with WEBSOCKET_TRUSTED_ORIGINS.
var SessionCookieTrustedURLs = loadWebSocketTrustedOrigins()

// NormalizeOrigin validates and canonicalizes a browser origin. Only an exact
// scheme, host and effective-port match is accepted; paths and wildcards are
// not valid origins for credential-bearing WebSocket handshakes.
func NormalizeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || strings.ContainsAny(raw, "\r\n") {
		return "", fmt.Errorf("origin is empty or invalid")
	}
	parsedURL, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid origin: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("origin scheme must be http or https")
	}
	if parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" || (parsedURL.Path != "" && parsedURL.Path != "/") {
		return "", fmt.Errorf("origin must contain only scheme and host")
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hostname == "" || strings.Contains(hostname, "*") {
		return "", fmt.Errorf("origin host is empty")
	}
	port := parsedURL.Port()
	normalizedHost := hostname
	if strings.Contains(hostname, ":") {
		normalizedHost = "[" + hostname + "]"
	}
	if port == "" || (parsedURL.Scheme == "http" && port == "80") || (parsedURL.Scheme == "https" && port == "443") {
		return parsedURL.Scheme + "://" + normalizedHost, nil
	}
	return parsedURL.Scheme + "://" + net.JoinHostPort(hostname, port), nil
}

func loadWebSocketTrustedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("WEBSOCKET_TRUSTED_ORIGINS"))
	if raw == "" {
		return nil
	}
	trusted := make([]string, 0)
	for _, candidate := range strings.Split(raw, ",") {
		normalized, err := NormalizeOrigin(candidate)
		if err == nil {
			trusted = append(trusted, normalized)
		}
	}
	return trusted
}
