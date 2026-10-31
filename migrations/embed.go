// Package migrations embeds the SQL migrations applied by golang-migrate.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
