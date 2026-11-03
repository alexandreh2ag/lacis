package netaddr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListen(t *testing.T) {
	tests := []struct {
		addr, defaultPort, want string
	}{
		// nginx
		{"80", "80", "0.0.0.0:80"},
		{"*:80", "80", "0.0.0.0:80"},
		{"*", "80", "0.0.0.0:80"},
		{"[::]:443", "80", "[::]:443"},
		{"[::1]", "80", "[::1]:80"},
		{"127.0.0.1:8080", "80", "127.0.0.1:8080"},
		{"localhost", "80", "localhost:80"},
		{"unix:/run/nginx.sock", "80", "unix:/run/nginx.sock"},
		// Apache
		{"_default_:443", "80", "0.0.0.0:443"},
		{"_default_", "80", "0.0.0.0:80"},
		{"10.0.1.5", "80", "10.0.1.5:80"},
		// HAProxy
		{":443", "", "0.0.0.0:443"},
		{"ipv6@:::443", "", ":::443"},
		{"ipv4@10.0.0.1:80", "", "10.0.0.1:80"},
		{"/run/haproxy/admin.sock", "", "unix:/run/haproxy/admin.sock"},
		{"unix@/run/haproxy/other.sock", "", "unix:/run/haproxy/other.sock"},
		{"[::1]", "", "[::1]"},
		// PHP-FPM
		{"9000", "", "0.0.0.0:9000"},
		{"/run/php/php8.2-fpm.sock", "", "unix:/run/php/php8.2-fpm.sock"},
		{"name", "", "name"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Listen(tt.addr, tt.defaultPort), tt.addr)
	}
}

func TestKey(t *testing.T) {
	for in, want := range map[string]string{
		"unix:/run/a.sock":          "unix:/run/a.sock",
		"http://localhost:3000/api": "127.0.0.1:3000",
		"127.0.1.1:3000":            "127.0.0.1:3000",
		"[::1]:9000":                "127.0.0.1:9000",
		"[fe80::1]:80":              "[fe80::1]:80",
		"[fe80::1]":                 "[fe80::1]",
		"backend":                   "backend",
	} {
		assert.Equal(t, want, Key(in), in)
	}
}
