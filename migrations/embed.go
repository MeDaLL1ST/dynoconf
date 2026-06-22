// Package migrations embeds the SQL migration files so they can be applied
// from inside the single binary at startup (or via the `migrate` subcommand).
package migrations

import "embed"

// FS holds the embedded *.sql migration files.
//
//go:embed *.sql
var FS embed.FS
