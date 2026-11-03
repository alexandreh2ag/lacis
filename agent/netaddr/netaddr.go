// Package netaddr is the one normalizer of the addresses the agent reads
// in configurations: nginx listen, Apache VirtualHost, HAProxy bind,
// PHP-FPM listen, and the targets an app is compared against.
package netaddr

import (
	"strings"
)

// Wildcard is the address listening on every IPv4 address.
const Wildcard = "0.0.0.0"

// Listen normalizes a listening address to "host:port" or "unix:/path":
//
//   - "/run/x.sock", "unix@/run/x.sock" (HAProxy) give "unix:/run/x.sock";
//   - the "ipv4@", "ipv6@"… prefixes of HAProxy are dropped;
//   - a port alone ("80", ":80") and the wildcards "*" and "_default_"
//     listen on every IPv4 address: "0.0.0.0:80";
//   - a host without port gets defaultPort, when given: "localhost:80".
//
// IPv6 addresses keep their form: "[::]:443", ":::443".
func Listen(addr, defaultPort string) string {
	switch {
	case strings.HasPrefix(addr, "unix:"):
		return addr
	case strings.HasPrefix(addr, "unix@"):
		return "unix:" + strings.TrimPrefix(addr, "unix@")
	case strings.HasPrefix(addr, "/"):
		return "unix:" + addr
	}
	if _, rest, ok := strings.Cut(addr, "@"); ok {
		addr = rest // ipv4@, ipv6@…
	}
	if strings.HasPrefix(addr, "[") {
		if strings.HasSuffix(addr, "]") && defaultPort != "" {
			return addr + ":" + defaultPort
		}
		return addr
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		switch {
		case isPort(addr):
			return Wildcard + ":" + addr
		case addr == "*" || addr == "_default_":
			addr = Wildcard
		}
		if defaultPort == "" {
			return addr
		}
		return addr + ":" + defaultPort
	}
	host, port := addr[:i], addr[i+1:]
	if host == "" || host == "*" || host == "_default_" {
		host = Wildcard
	}
	return host + ":" + port
}

func isPort(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// Key returns the form under which two addresses of a same host are
// compared: scheme and path dropped, loopback names as 127.0.0.1, IPv6 in
// brackets. "http://localhost:3000/api" and "127.0.0.1:3000" have the same
// key; a unix socket is its own key.
func Key(s string) string {
	if strings.HasPrefix(s, "unix:") {
		return s
	}
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	s, _, _ = strings.Cut(s, "/")
	host, port := s, ""
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.HasSuffix(s, "]") {
		host, port = s[:i], s[i+1:]
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.") {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}
