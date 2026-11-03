package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInsecureRemote(t *testing.T) {
	tests := []struct {
		dsn      string
		host     string
		insecure bool
	}{
		{dsn: "postgres://lacis@db.acme.example/lacis?sslmode=disable", host: "db.acme.example", insecure: true},
		{dsn: "host=10.0.0.5 user=lacis dbname=lacis sslmode=disable", host: "10.0.0.5", insecure: true},
		{dsn: "postgres://lacis@db.acme.example/lacis?sslmode=verify-full"},
		{dsn: "postgres://lacis@db.acme.example/lacis"},
		{dsn: "postgres://lacis@localhost/lacis?sslmode=disable", host: "localhost"},
		{dsn: "postgres://lacis@127.0.0.1:5432/lacis?sslmode=disable", host: "127.0.0.1"},
		{dsn: "postgres://lacis@[::1]:5432/lacis?sslmode=disable", host: "::1"},
		{dsn: "host=/run/postgresql user=lacis sslmode=disable", host: "/run/postgresql"},
		{dsn: "postgres://%zz"},
	}
	for _, tt := range tests {
		t.Run(tt.dsn, func(t *testing.T) {
			host, insecure := InsecureRemote(tt.dsn)
			assert.Equal(t, tt.insecure, insecure)
			assert.Equal(t, tt.host, host)
		})
	}
}
