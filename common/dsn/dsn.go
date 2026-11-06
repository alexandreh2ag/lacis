// Package dsn parses the connection strings found in application configs
// (URLs, PDO DSNs, libpq key/value strings, host:port) and keeps only what is
// needed to link an app to its dependencies.
//
// Credentials and query strings never leave this package: a parsed DSN only
// carries the scheme, the host, the port, the resource name of the schemes
// that have one (database name, Redis index, AMQP vhost), the path of an
// HTTP URL (the route it calls) and, for local connections, the unix socket.
// The path of any other URL is dropped. The path of an HTTP URL may hold a
// token, as in a webhook URL: its key is to be denied.
package dsn

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the kind of a dependency target.
type Kind string

const (
	KindIP     Kind = "ip"
	KindName   Kind = "name"
	KindSocket Kind = "socket"
)

var (
	ErrEmpty         = errors.New("empty value")
	ErrUnparsable    = errors.New("unparsable value")
	ErrNotNetwork    = errors.New("not a network dependency")
	ErrMultipleHosts = errors.New("multiple hosts are not supported")
)

// DSN is a connection string stripped of its credentials.
type DSN struct {
	Scheme string
	// Host is lower case, without brackets nor trailing dot. It is empty when
	// the connection goes through Socket.
	Host string
	Port int
	// DefaultPort is true when Port was not in the value but deduced from the
	// scheme.
	DefaultPort bool
	// Path is the database name, the RabbitMQ vhost or the Redis index; it
	// is only set for the schemes of PathSchemes, and for PDO and libpq
	// strings.
	Path string
	// URLPath is the path of an HTTP URL, such as /api/v1: the route it
	// calls; empty for the root.
	URLPath string
	Socket  string
}

// Kind returns the kind of target of the DSN.
func (d DSN) Kind() Kind {
	if d.Socket != "" {
		return KindSocket
	}
	return HostKind(d.Host)
}

// Target returns the value to resolve: the socket path or the host.
func (d DSN) Target() string {
	if d.Socket != "" {
		return d.Socket
	}
	return d.Host
}

// HostKind tells whether host is an IP literal or a name.
func HostKind(host string) Kind {
	if net.ParseIP(host) != nil {
		return KindIP
	}
	return KindName
}

var defaultPorts = map[string]int{
	"amqp":          5672,
	"amqps":         5671,
	"clickhouse":    9000,
	"elasticsearch": 9200,
	"ftp":           21,
	"http":          80,
	"https":         443,
	"kafka":         9092,
	"ldap":          389,
	"ldaps":         636,
	"mariadb":       3306,
	"memcached":     11211,
	"mongodb":       27017,
	"mysql":         3306,
	"mysqli":        3306,
	"nats":          4222,
	"pdo-mysql":     3306,
	"pdo-pgsql":     5432,
	"pgsql":         5432,
	"postgres":      5432,
	"postgresql":    5432,
	"redis":         6379,
	"rediss":        6379,
	"sftp":          22,
	"smtp":          25,
	"smtps":         465,
	"ssh":           22,
	"ws":            80,
	"wss":           443,
}

// DefaultPort returns the usual port of scheme, or 0 when it is unknown.
func DefaultPort(scheme string) int {
	return defaultPorts[strings.ToLower(scheme)]
}

// nonNetworkSchemes are transports that never reach another process, such as
// the Symfony Messenger sync and doctrine transports.
var nonNetworkSchemes = map[string]bool{
	"doctrine":  true,
	"file":      true,
	"in-memory": true,
	"native":    true,
	"null":      true,
	"sendmail":  true,
	"sqlite":    true,
	"sqlite3":   true,
	"sync":      true,
}

// HTTPSchemes are the schemes whose URL path is a route of the target: it
// is kept as the URLPath.
var HTTPSchemes = map[string]bool{"http": true, "https": true, "ws": true, "wss": true}

// PathSchemes are the schemes whose URL path names a resource of the
// target (a database, a Redis index, an AMQP vhost) rather than something
// the target serves: the path of any other URL than those and HTTPSchemes
// is dropped.
var PathSchemes = map[string]bool{
	"amqp":        true,
	"amqps":       true,
	"clickhouse":  true,
	"mariadb":     true,
	"mongodb":     true,
	"mongodb+srv": true,
	"mysql":       true,
	"mysqli":      true,
	"pdo-mysql":   true,
	"pdo-pgsql":   true,
	"pgsql":       true,
	"postgres":    true,
	"postgresql":  true,
	"redis":       true,
	"rediss":      true,
}

var (
	schemeRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)
	hostnameRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*$`)
)

// Parse parses a URL ("postgresql://user:pass@db:5432/app?sslmode=require"),
// a PDO DSN ("mysql:host=db;port=3306;dbname=app"), a libpq key/value string
// ("host=db port=5432 dbname=app") or a bare "host[:port]".
func Parse(raw string) (DSN, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DSN{}, ErrEmpty
	}
	switch {
	case strings.Contains(value, "://"):
		return parseURL(value)
	case isPDO(value):
		return parsePDO(value)
	case strings.Contains(value, "="):
		return parseKeyValue("", strings.Fields(value))
	case strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../"):
		// A path: a unix socket, else a path of the app (/api/auth).
		if isSocketPath(value) {
			return DSN{Socket: path.Clean(value)}, nil
		}
		return DSN{}, fmt.Errorf("path %q: %w", value, ErrNotNetwork)
	default:
		host, port, err := ParseHostPort(value)
		if err != nil {
			return DSN{}, err
		}
		return DSN{Host: host, Port: port}, nil
	}
}

func parseURL(value string) (DSN, error) {
	sep := strings.Index(value, "://")
	scheme := strings.ToLower(value[:sep])
	if !schemeRe.MatchString(scheme) {
		return DSN{}, fmt.Errorf("scheme %q: %w", scheme, ErrUnparsable)
	}
	if nonNetworkSchemes[scheme] {
		return DSN{}, fmt.Errorf("scheme %q: %w", scheme, ErrNotNetwork)
	}
	// The credentials go before url.Parse, which would take the "/" of a
	// password for the end of the host.
	value = value[:sep+len("://")] + withoutUserinfo(value[sep+len("://"):])
	// Checked before url.Parse, whose handling of "a:1,b:2" depends on the
	// Go version.
	if strings.Contains(authority(value), ",") {
		return DSN{}, ErrMultipleHosts
	}
	u, err := url.Parse(strayPercent.ReplaceAllString(value, "%25${1}"))
	if err != nil {
		return DSN{}, fmt.Errorf("%w: %s", ErrUnparsable, scrub(err))
	}

	d := DSN{Scheme: scheme}
	if p := strings.Trim(u.Path, "/"); p != "" {
		if scheme == "unix" {
			d.Socket = path.Clean(u.Path)
			return d, nil
		}
		if PathSchemes[scheme] {
			d.Path = path.Base(p)
		}
		if HTTPSchemes[scheme] {
			d.URLPath = u.Path
		}
	}
	if socket := socketFromQuery(u.Query()); socket != "" {
		d.Socket = socket
		return d, nil
	}
	if u.Host == "" {
		if scheme == "unix" && u.Opaque != "" {
			d.Socket = path.Clean(u.Opaque)
			return d, nil
		}
		// redis:///run/redis/redis.sock: a socket path without host, told
		// apart from a database name (mysql:///app) by its depth or suffix.
		if p := strings.Trim(u.Path, "/"); strings.Contains(p, "/") || strings.HasSuffix(p, ".sock") {
			d.Socket, d.Path = path.Clean(u.Path), ""
			return d, nil
		}
		return DSN{}, fmt.Errorf("no host: %w", ErrUnparsable)
	}
	host, err := normalizeHost(u.Hostname())
	if err != nil {
		return DSN{}, err
	}
	d.Host = host
	if p := u.Port(); p != "" {
		if d.Port, err = parsePort(p); err != nil {
			return DSN{}, err
		}
	} else if d.Port = DefaultPort(scheme); d.Port != 0 {
		d.DefaultPort = true
	}
	return d, nil
}

// withoutUserinfo removes the credentials of rest, the part of a URL after
// "://": everything up to its last "@". A password may hold any of "/",
// "?", "#" or "@", so the last "@" is the only reliable end of the
// credentials; an "@" in the path or the query of a URL then makes a wrong
// target, never a leaked password.
func withoutUserinfo(rest string) string {
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		return rest[i+1:]
	}
	return rest
}

// strayPercent is a "%" that starts no escape, such as the "%s" of a
// printf template: it is kept as a character.
var strayPercent = regexp.MustCompile(`%([^0-9A-Fa-f]|[0-9A-Fa-f][^0-9A-Fa-f]|[0-9A-Fa-f]?$)`)

// isSocketPath tells a unix socket from another absolute path.
func isSocketPath(p string) bool {
	return strings.HasSuffix(p, ".sock") || strings.HasSuffix(p, ".socket") ||
		strings.HasPrefix(p, "/run/") || strings.HasPrefix(p, "/var/run/") || strings.HasPrefix(p, "/tmp/")
}

// ParseAll parses like Parse, but a URL with several hosts, such as a
// MongoDB replica set or a Redis cluster, gives one DSN per host.
func ParseAll(raw string) ([]DSN, error) {
	d, err := Parse(raw)
	if !errors.Is(err, ErrMultipleHosts) {
		if err != nil {
			return nil, err
		}
		return []DSN{d}, nil
	}
	value := strings.TrimSpace(raw)
	sep := strings.Index(value, "://")
	if sep < 0 {
		return nil, err
	}
	head, rest := value[:sep+3], withoutUserinfo(value[sep+3:])
	end := len(rest)
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		end = i
	}
	hosts := rest[:end]
	var out []DSN
	for _, host := range strings.Split(hosts, ",") {
		one, err := Parse(head + host + rest[end:])
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

// authority returns the hosts part of a URL, without the credentials.
func authority(value string) string {
	rest := withoutUserinfo(value[strings.Index(value, "://")+len("://"):])
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// socketFromQuery reads the unix socket of a URL, which drivers take from the
// query string (unix_socket for MySQL, host for libpq).
func socketFromQuery(q url.Values) string {
	for _, key := range []string{"unix_socket", "socket", "host"} {
		if v := q.Get(key); strings.HasPrefix(v, "/") {
			return path.Clean(v)
		}
	}
	return ""
}

var pdoRe = regexp.MustCompile(`^[a-z][a-z0-9]*:[a-z_]+=`)

func isPDO(value string) bool {
	return pdoRe.MatchString(value)
}

func parsePDO(value string) (DSN, error) {
	i := strings.Index(value, ":")
	scheme := strings.ToLower(value[:i])
	if nonNetworkSchemes[scheme] {
		return DSN{}, fmt.Errorf("scheme %q: %w", scheme, ErrNotNetwork)
	}
	return parseKeyValue(scheme, strings.Split(value[i+1:], ";"))
}

func parseKeyValue(scheme string, pairs []string) (DSN, error) {
	d := DSN{Scheme: scheme}
	values := map[string]string{}
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return DSN{}, fmt.Errorf("pair without '=': %w", ErrUnparsable)
		}
		values[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `'"`)
	}
	if v := values["dbname"]; v != "" {
		d.Path = v
	} else if v := values["database"]; v != "" {
		d.Path = v
	}
	if v := values["unix_socket"]; v != "" {
		d.Socket = path.Clean(v)
		return d, nil
	}
	host := values["host"]
	if host == "" {
		host = values["hostaddr"]
	}
	if strings.HasPrefix(host, "/") {
		d.Socket = path.Clean(host)
		return d, nil
	}
	if host == "" {
		return DSN{}, fmt.Errorf("no host: %w", ErrUnparsable)
	}
	if strings.Contains(host, ",") {
		return DSN{}, ErrMultipleHosts
	}
	var err error
	if d.Host, err = normalizeHost(host); err != nil {
		return DSN{}, err
	}
	if p := values["port"]; p != "" {
		if d.Port, err = parsePort(p); err != nil {
			return DSN{}, err
		}
	} else if d.Port = DefaultPort(scheme); d.Port != 0 {
		d.DefaultPort = true
	}
	return d, nil
}

// ParseHostPort parses "host", "host:port", "[ipv6]:port" or a bare IPv6.
// The port is 0 when absent.
func ParseHostPort(value string) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, ErrEmpty
	}
	if strings.Contains(value, ",") {
		return "", 0, ErrMultipleHosts
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		host, err := normalizeHost(value)
		return host, 0, err
	}
	host, portStr := value, ""
	if strings.Contains(value, ":") {
		var err error
		if host, portStr, err = net.SplitHostPort(value); err != nil {
			return "", 0, fmt.Errorf("%w: %s", ErrUnparsable, scrub(err))
		}
	}
	host, err := normalizeHost(host)
	if err != nil {
		return "", 0, err
	}
	port := 0
	if portStr != "" {
		if port, err = parsePort(portStr); err != nil {
			return "", 0, err
		}
	}
	return host, port, nil
}

// NormalizeHost lower-cases host and removes brackets and the trailing dot.
func NormalizeHost(host string) (string, error) {
	return normalizeHost(host)
}

func normalizeHost(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")), ".")
	if host == "" {
		return "", fmt.Errorf("no host: %w", ErrUnparsable)
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	if len(host) > 253 || !hostnameRe.MatchString(host) {
		return "", fmt.Errorf("invalid host: %w", ErrUnparsable)
	}
	return host, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port: %w", ErrUnparsable)
	}
	return port, nil
}

// scrub keeps only the reason of a parse error: url and net errors quote the
// input, which can hold a password.
func scrub(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var addrErr *net.AddrError
	if errors.As(err, &addrErr) {
		return addrErr.Err
	}
	msg := err.Error()
	if i := strings.Index(msg, " \""); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
