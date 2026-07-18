package migrations

import "embed"

// FS contains the versioned SQL used by both the CLI and deployments.
//
//go:embed *.sql
var FS embed.FS
