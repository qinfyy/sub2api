// Package sqlitev1 contains reviewed, versioned SQLite migrations.
package sqlitev1

import "embed"

//go:embed *.sql
var FS embed.FS
