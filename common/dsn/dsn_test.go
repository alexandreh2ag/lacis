package dsn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    DSN
		wantErr error
	}{
		{
			name: "postgres url with credentials and query",
			raw:  "postgresql://app:s3cr3t@DB.Internal.:5432/client_staging?sslmode=require&password=x",
			want: DSN{Scheme: "postgresql", Host: "db.internal", Port: 5432, Path: "client_staging"},
		},
		{
			name: "default port from scheme",
			raw:  "redis://:pass@10.0.1.21/0",
			want: DSN{Scheme: "redis", Host: "10.0.1.21", Port: 6379, DefaultPort: true, Path: "0"},
		},
		{
			name: "unknown scheme without port",
			raw:  "custom://svc.local",
			want: DSN{Scheme: "custom", Host: "svc.local"},
		},
		{
			name: "ipv6 literal",
			raw:  "mysql://root@[2001:DB8::1]:3307/app",
			want: DSN{Scheme: "mysql", Host: "2001:db8::1", Port: 3307, Path: "app"},
		},
		{
			name: "https api with path",
			raw:  "https://api.client.fr/v1/endpoint",
			want: DSN{Scheme: "https", Host: "api.client.fr", Port: 443, DefaultPort: true, Path: "endpoint"},
		},
		{
			name: "rabbitmq vhost",
			raw:  "amqp://guest:guest@rabbit:5672/client",
			want: DSN{Scheme: "amqp", Host: "rabbit", Port: 5672, Path: "client"},
		},
		{
			name: "mysql socket in query",
			raw:  "mysql://app:pwd@localhost/app?unix_socket=/run/mysqld/mysqld.sock",
			want: DSN{Scheme: "mysql", Path: "app", Socket: "/run/mysqld/mysqld.sock"},
		},
		{
			name: "libpq socket in host query",
			raw:  "postgresql://app@/app?host=/var/run/postgresql",
			want: DSN{Scheme: "postgresql", Path: "app", Socket: "/var/run/postgresql"},
		},
		{
			name: "unix url",
			raw:  "unix:///run/php/php8.2-fpm.sock",
			want: DSN{Scheme: "unix", Socket: "/run/php/php8.2-fpm.sock"},
		},
		{
			name: "pdo mysql",
			raw:  "mysql:host=db.local;port=3306;dbname=app;charset=utf8mb4",
			want: DSN{Scheme: "mysql", Host: "db.local", Port: 3306, Path: "app"},
		},
		{
			name: "pdo pgsql default port",
			raw:  "pgsql:host=10.0.1.20;dbname=app",
			want: DSN{Scheme: "pgsql", Host: "10.0.1.20", Port: 5432, DefaultPort: true, Path: "app"},
		},
		{
			name: "pdo unix socket",
			raw:  "mysql:unix_socket=/run/mysqld/mysqld.sock;dbname=app",
			want: DSN{Scheme: "mysql", Path: "app", Socket: "/run/mysqld/mysqld.sock"},
		},
		{
			name: "libpq key value",
			raw:  "host=db port=5433 dbname=app user=app password=secret",
			want: DSN{Host: "db", Port: 5433, Path: "app"},
		},
		{
			name: "host and port",
			raw:  "redis.internal:6380",
			want: DSN{Host: "redis.internal", Port: 6380},
		},
		{
			name: "bare ip",
			raw:  "10.0.1.20",
			want: DSN{Host: "10.0.1.20"},
		},
		{name: "empty", raw: "  ", wantErr: ErrEmpty},
		{name: "messenger sync transport", raw: "sync://", wantErr: ErrNotNetwork},
		{name: "doctrine transport", raw: "doctrine://default?queue_name=async", wantErr: ErrNotNetwork},
		{name: "sqlite pdo", raw: "sqlite:path=/var/data.db", wantErr: ErrNotNetwork},
		{name: "multiple hosts", raw: "mongodb://a:27017,b:27017/app", wantErr: ErrMultipleHosts},
		{
			name: "comma in password",
			raw:  "mysql://app:p,ss@db:3306/app",
			want: DSN{Scheme: "mysql", Host: "db", Port: 3306, Path: "app"},
		},
		{name: "invalid port", raw: "mysql://db:99999/app", wantErr: ErrUnparsable},
		{name: "no host", raw: "mysql:///app", wantErr: ErrUnparsable},
		{name: "printf template in path", raw: "https://media.reflet.dev/prc/%s/mail/", want: DSN{Scheme: "https", Host: "media.reflet.dev", Port: 443, DefaultPort: true, Path: "mail"}},
		{name: "escape kept", raw: "amqp://10.0.0.1:5672/%2fvhost", want: DSN{Scheme: "amqp", Host: "10.0.0.1", Port: 5672, Path: "vhost"}},
		{name: "socket path", raw: "/var/run/mysqld/mysqld.sock", want: DSN{Socket: "/var/run/mysqld/mysqld.sock"}},
		{name: "run path", raw: "/run/redis/redis-server", want: DSN{Socket: "/run/redis/redis-server"}},
		{name: "app path", raw: "/api/auth", wantErr: ErrNotNetwork},
		{name: "relative path", raw: "./var/data", wantErr: ErrNotNetwork},
		{name: "socket without host", raw: "redis:///run/redis/redis.sock", want: DSN{Scheme: "redis", Socket: "/run/redis/redis.sock"}},
		{name: "sock file at root", raw: "redis:///redis.sock", want: DSN{Scheme: "redis", Socket: "/redis.sock"}},
		{name: "invalid host", raw: "db host:3306", wantErr: ErrUnparsable},
		{name: "free text", raw: "some value", wantErr: ErrUnparsable},
		{name: "pdo without host", raw: "mysql:dbname=app", wantErr: ErrUnparsable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParse_ErrorsNeverContainCredentials(t *testing.T) {
	for _, raw := range []string{
		"mysql://user:Sup3rS3cret@db:notaport/app",
		"postgresql://user:Sup3rS3cret@db host/app",
		"Sup3rS3cret:1:2",
	} {
		_, err := Parse(raw)
		require.Error(t, err, raw)
		assert.NotContains(t, err.Error(), "Sup3rS3cret", raw)
	}
}

func TestDSN_KindAndTarget(t *testing.T) {
	assert.Equal(t, KindIP, DSN{Host: "10.0.0.1"}.Kind())
	assert.Equal(t, KindIP, DSN{Host: "::1"}.Kind())
	assert.Equal(t, KindName, DSN{Host: "db.internal"}.Kind())
	assert.Equal(t, KindSocket, DSN{Socket: "/run/x.sock"}.Kind())
	assert.Equal(t, "/run/x.sock", DSN{Socket: "/run/x.sock"}.Target())
	assert.Equal(t, "db", DSN{Host: "db"}.Target())
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		raw      string
		wantHost string
		wantPort int
		wantErr  error
	}{
		{raw: "db", wantHost: "db"},
		{raw: "db:5432", wantHost: "db", wantPort: 5432},
		{raw: "[::1]:6379", wantHost: "::1", wantPort: 6379},
		{raw: "::1", wantHost: "::1"},
		{raw: "", wantErr: ErrEmpty},
		{raw: "a,b", wantErr: ErrMultipleHosts},
		{raw: "db:0", wantErr: ErrUnparsable},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			host, port, err := ParseHostPort(tt.raw)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	host, err := NormalizeHost("  API.Client.FR. ")
	require.NoError(t, err)
	assert.Equal(t, "api.client.fr", host)

	_, err = NormalizeHost("")
	assert.ErrorIs(t, err, ErrUnparsable)
}

func TestDefaultPort(t *testing.T) {
	assert.Equal(t, 5432, DefaultPort("PostgreSQL"))
	assert.Equal(t, 0, DefaultPort("unknown"))
}

func TestParseAll(t *testing.T) {
	got, err := ParseAll("mongodb://u:p@172.16.1.144:27017,172.16.1.145:27017,[fd00::1]/?replicaSet=rs&authMechanism=SCRAM-SHA-256")
	require.NoError(t, err)
	assert.Equal(t, []DSN{
		{Scheme: "mongodb", Host: "172.16.1.144", Port: 27017},
		{Scheme: "mongodb", Host: "172.16.1.145", Port: 27017},
		{Scheme: "mongodb", Host: "fd00::1", Port: 27017, DefaultPort: true},
	}, got)

	got, err = ParseAll("redis://10.0.0.1:6379")
	require.NoError(t, err)
	assert.Len(t, got, 1)

	_, err = ParseAll("a,b")
	assert.ErrorIs(t, err, ErrMultipleHosts)
	_, err = ParseAll("")
	assert.ErrorIs(t, err, ErrEmpty)
	_, err = ParseAll("mongodb://a:1,b:x/db")
	assert.Error(t, err)
}
